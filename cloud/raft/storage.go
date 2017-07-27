package raft

import (
    "sync"
    "errors"

    "devicedb"
    "github.com/coreos/etcd/raft"
    "github.com/coreos/etcd/raft/raftpb"
    "encoding/binary"
)

var KeySnapshot = []byte{ 0 }
var KeyHardState = []byte{ 1 }
var KeyPrefixEntry = []byte{ 2 }

func entryKey(index uint64) []byte {
    key := make([]byte, 0, len(KeyPrefixEntry) + 8)
    indexBytes := make([]byte, 8)

    binary.BigEndian.PutUint64(indexBytes, index)

    key = append(key, KeyPrefixEntry...)
    key = append(key, indexBytes...)

    return key
}

func entryIndex(e []byte) (uint64, error) {
    if len(e) != len(KeyPrefixEntry) + 8 {
        return 0, errors.New("Unable to decode entry key")
    }

    return binary.BigEndian.Uint64(e[len(KeyPrefixEntry):]), nil
}

// it is up to the caller to ensure lastIndex >= firstIndex
func entryKeys(firstIndex, lastIndex uint64) [][]byte {
    if firstIndex > lastIndex {
        return [][]byte{ }
    }

    keys := make([][]byte, lastIndex - firstIndex + 1)

    for i := firstIndex; i <= lastIndex; i++ {
        keys[i - firstIndex] = entryKey(i)
    }

    return keys
}

type RaftStorage struct {
    isOpen bool
    isEmpty bool
    storageDriver devicedb.StorageDriver
    lock sync.Mutex
    memoryStorage *raft.MemoryStorage
}

func NewRaftStorage(storageDriver devicedb.StorageDriver) *RaftStorage {
    return &RaftStorage{
        isEmpty: true,
        storageDriver: storageDriver,
        memoryStorage: raft.NewMemoryStorage(),
    }
}

func (raftStorage *RaftStorage) cloneMemoryStorage() raft.MemoryStorage {
    return *raftStorage.memoryStorage
}

func (raftStorage *RaftStorage) restoreMemoryStorage(s raft.MemoryStorage) {
    *raftStorage.memoryStorage = s
}

func (raftStorage *RaftStorage) Open() error {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    if err := raftStorage.storageDriver.Open(); err != nil {
        return err
    }

    // reset memory storage
    raftStorage.memoryStorage = raft.NewMemoryStorage()

    snapshotGetResults, err := raftStorage.storageDriver.Get([][]byte{ KeySnapshot })

    if err != nil {
        return err
    }

    if snapshotGetResults[0] != nil {
        var snapshot raftpb.Snapshot

        err := snapshot.Unmarshal(snapshotGetResults[0])

        if err != nil {
            return err
        }

        err = raftStorage.memoryStorage.ApplySnapshot(snapshot)

        if err != nil {
            return err
        }

        raftStorage.isEmpty = false
    }

    hardStateGetResults, err := raftStorage.storageDriver.Get([][]byte{ KeyHardState })

    if err != nil {
        return err
    }

    if hardStateGetResults[0] != nil {
        var hardState raftpb.HardState

        err := hardState.Unmarshal(hardStateGetResults[0])

        if err != nil {
            return err
        }

        err = raftStorage.memoryStorage.SetHardState(hardState)

        if err != nil {
            return err
        }

        raftStorage.isEmpty = false
    }

    entriesIterator, err := raftStorage.storageDriver.GetMatches([][]byte{ KeyPrefixEntry })

    if err != nil {
        return err
    }

    var entries = []raftpb.Entry{ }
    var lastEntryIndex *uint64 = nil

    for entriesIterator.Next() {
        var entry raftpb.Entry

        ek := entriesIterator.Key()
        encodedEntry := entriesIterator.Value()
        expectedEntryIndex, err := entryIndex(ek)

        if err != nil {
            return err
        }

        if lastEntryIndex != nil && *lastEntryIndex + 1 != expectedEntryIndex {
            return errors.New("Entry indices are not monotonically increasing")
        }

        lastEntryIndex = &expectedEntryIndex

        err = entry.Unmarshal(encodedEntry)

        if err != nil {
            return err
        }

        if entry.Index != expectedEntryIndex {
            return errors.New("Encoded entry index does not match index in its key")
        }

        entries = append(entries, entry)
        raftStorage.isEmpty = false
    }

    if entriesIterator.Error() != nil {
        return entriesIterator.Error()
    }

    err = raftStorage.memoryStorage.Append(entries)

    if err != nil {
        return err
    }

    raftStorage.isOpen = true

    return nil
}

func (raftStorage *RaftStorage) Close() error {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    raftStorage.isOpen = false
    raftStorage.isEmpty = true
    raftStorage.memoryStorage = raft.NewMemoryStorage()

    return raftStorage.storageDriver.Close()
}

func (raftStorage *RaftStorage) IsEmpty() bool {
    return raftStorage.isEmpty
}

// START raft.Storage interface methods
func (raftStorage *RaftStorage) InitialState() (raftpb.HardState, raftpb.ConfState, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.InitialState()
}

func (raftStorage *RaftStorage) Entries(lo, hi, maxSize uint64) ([]raftpb.Entry, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.Entries(lo, hi, maxSize)
}

func (raftStorage *RaftStorage) Term(i uint64) (uint64, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.Term(i)
}

func (raftStorage *RaftStorage) LastIndex() (uint64, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.LastIndex()
}

func (raftStorage *RaftStorage) FirstIndex() (uint64, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.FirstIndex()
}

func (raftStorage *RaftStorage) Snapshot() (raftpb.Snapshot, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    return raftStorage.memoryStorage.Snapshot()
}
// END raft.Storage interface methods

func (raftStorage *RaftStorage) SetHardState(st raftpb.HardState) error {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    memoryStorageCopy := raftStorage.cloneMemoryStorage()
    err := raftStorage.memoryStorage.SetHardState(st)

    if err != nil {
        return err
    }

    if !raftStorage.isOpen {
        return nil
    }

    encodedHardState, err := st.Marshal()

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return err
    }

    storageBatch := devicedb.NewBatch()
    storageBatch.Put(KeyHardState, encodedHardState)

    err = raftStorage.storageDriver.Batch(storageBatch)

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return err
    }

    return nil
}

func (raftStorage *RaftStorage) ApplySnapshot(snap raftpb.Snapshot) error {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    memoryStorageCopy := raftStorage.cloneMemoryStorage()
    err := raftStorage.memoryStorage.ApplySnapshot(snap)

    if err != nil {
        return err
    }

    if !raftStorage.isOpen {
        return nil
    }

    firstIndex, _ := memoryStorageCopy.FirstIndex()
    lastIndex, _ := memoryStorageCopy.LastIndex()

    purgedEntryKeys := entryKeys(firstIndex, lastIndex)
    encodedSnap, err := snap.Marshal()

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return err
    }

    storageBatch := devicedb.NewBatch()

    for _, purgedEntryKey := range purgedEntryKeys {
        storageBatch.Delete(purgedEntryKey)
    }

    storageBatch.Put(KeySnapshot, encodedSnap)

    err = raftStorage.storageDriver.Batch(storageBatch)

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return err
    }

    return nil
}

// Atomically take a snapshot of the current state and compact the entries up to the point that the snapshot was taken
func (raftStorage *RaftStorage) CreateSnapshot(i uint64, cs *raftpb.ConfState, data []byte) (raftpb.Snapshot, error) {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    // clone current state of memoryStorage so if persisting the snapshot to disk doesn't work we can
    // revert memoryStorage to its original state
    memoryStorageCopy := raftStorage.cloneMemoryStorage()
    originalFirstIndex, _ := raftStorage.memoryStorage.FirstIndex()
    snap, err := raftStorage.memoryStorage.CreateSnapshot(i, cs, data)

    if err != nil {
        return raftpb.Snapshot{ }, err
    }

    err = raftStorage.memoryStorage.Compact(i)

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return raftpb.Snapshot{ }, err
    }

    // if raftStorage isn't open just treat raftStorage like memoryStorage
    if !raftStorage.isOpen {
        return snap, nil
    }

    encodedSnap, err := snap.Marshal()

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return raftpb.Snapshot{ }, err
    }

    newFirstIndex, _ := raftStorage.memoryStorage.FirstIndex()
    purgedEntryKeys := entryKeys(originalFirstIndex, newFirstIndex - 1)

    storageBatch := devicedb.NewBatch()

    for _, purgedEntryKey := range purgedEntryKeys {
        storageBatch.Delete(purgedEntryKey)
    }

    storageBatch.Put(KeySnapshot, encodedSnap)

    err = raftStorage.storageDriver.Batch(storageBatch)

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return raftpb.Snapshot{ }, err
    }

    return snap, nil
}

func (raftStorage *RaftStorage) Append(entries []raftpb.Entry) error {
    raftStorage.lock.Lock()
    defer raftStorage.lock.Unlock()

    if len(entries) == 0 {
        return nil
    }

    memoryStorageCopy := raftStorage.cloneMemoryStorage()
    originalFirstIndex, _ := raftStorage.memoryStorage.FirstIndex()
    originalLastIndex, _ := raftStorage.memoryStorage.LastIndex()

    if entries[0].Index + uint64(len(entries)) - 1 < originalFirstIndex {
        return nil
    }

    err := raftStorage.memoryStorage.Append(entries)

    if err != nil {
        return err
    }

    if !raftStorage.isOpen {
        return nil
    }

    // truncate compacted entries
    // ignores entries being appended whose index was previously compacted
    if originalFirstIndex > entries[0].Index {
        entries = entries[originalFirstIndex - entries[0].Index:]
    }

    storageBatch := devicedb.NewBatch()

    // purge all old entries whose index >= entires[0].Index
    for i := entries[0].Index; i <= originalLastIndex; i++ {
        ek := entryKey(i)

        storageBatch.Delete(ek)
    }

    // put all newly appended entries into the storage
    for i := 0; i < len(entries); i += 1 {
        ek := entryKey(entries[i].Index)
        encodedEntry, err := entries[i].Marshal()

        if err != nil {
            raftStorage.restoreMemoryStorage(memoryStorageCopy)

            return err
        }

        storageBatch.Put(ek, encodedEntry)
    }

    err = raftStorage.storageDriver.Batch(storageBatch)

    if err != nil {
        raftStorage.restoreMemoryStorage(memoryStorageCopy)

        return err
    }

    return nil
}