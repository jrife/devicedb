package raft_test

import (
    "devicedb"
    "time"

    "github.com/coreos/etcd/raft"
    "github.com/coreos/etcd/raft/raftpb"

    . "devicedb/cloud/raft"

    . "github.com/onsi/ginkgo"
    . "github.com/onsi/gomega"

    "golang.org/x/net/context"
)

var _ = Describe("Node", func() {
    It("should catch up with the rest of the nodes in the cluster if it is shut down then re-initialized", func() {
        node1 := NewRaftNode(&RaftNodeConfig{
            ID: 0x1,
            CreateClusterIfNotExist: true,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
        })

        node2 := NewRaftNode(&RaftNodeConfig{
            ID: 0x2,
            CreateClusterIfNotExist: false,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
        })

        node3 := NewRaftNode(&RaftNodeConfig{
            ID: 0x3,
            CreateClusterIfNotExist: false,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
        })

        nodeMap := map[uint64]*RaftNode{
            1: node1,
            2: node2,
            3: node3,
        }

        nodeEntriesMap := map[uint64][]raftpb.Entry{
            1: []raftpb.Entry{ },
            2: []raftpb.Entry{ },
            3: []raftpb.Entry{ },
        }

        var run = func(id uint64, node *RaftNode) {
            for {
                select {
                case msg := <-node.Messages():
                    nodeMap[msg.To].Receive(context.TODO(), msg)
                case <-node.Snapshots():
                case entry := <-node.Entries():
                    nodeEntriesMap[id] = append(nodeEntriesMap[id], entry)
                }
            }
        }

        go run(1, node1)
        go run(2, node2)
        go run(3, node3)

        Expect(node1.Start()).Should(BeNil())
        Expect(node2.Start()).Should(BeNil())
        Expect(node3.Start()).Should(BeNil())
        <-time.After(time.Second * 1)
        Expect(node1.AddNode(context.TODO(), 2)).Should(BeNil())
        <-time.After(time.Second * 1)
        Expect(node1.AddNode(context.TODO(), 3)).Should(BeNil())
        <-time.After(time.Second * 5)

        devicedb.Log.Infof("Propose random entries")
        go node1.Propose(context.TODO(), []byte(randomString()))
        go node2.Propose(context.TODO(), []byte(randomString()))
        go node3.Propose(context.TODO(), []byte(randomString()))
        go node1.Propose(context.TODO(), []byte(randomString()))
        go node2.Propose(context.TODO(), []byte(randomString()))
        go node3.Propose(context.TODO(), []byte(randomString()))
        <-time.After(time.Second * 5)
        devicedb.Log.Infof("Shutting down node 2")
        node2.Stop()
        devicedb.Log.Infof("Shut down node 2")

        devicedb.Log.Infof("Proposing random entires with nodes 1 and 3")
        <-time.After(time.Second * 5)
        go node1.Propose(context.TODO(), []byte(randomString()))
        go node3.Propose(context.TODO(), []byte(randomString()))
        <-time.After(time.Second * 5)
        devicedb.Log.Infof("Restarting node 2")
        // clear out the entries received so far since we should expect the entires to be replayed
        nodeEntriesMap[2] = []raftpb.Entry{ }
        Expect(node2.Start()).Should(BeNil())
        devicedb.Log.Infof("Restarted node 2")
        <-time.After(time.Second * 5)

        Expect(nodeEntriesMap[1]).Should(Equal(nodeEntriesMap[2]))
        Expect(nodeEntriesMap[2]).Should(Equal(nodeEntriesMap[3]))
    })

    It("should trigger snapshot", func() {
        getSnapshot := func() ([]byte, error) {
            return []byte{ }, nil
        }

        node1 := NewRaftNode(&RaftNodeConfig{
            ID: 0x1,
            CreateClusterIfNotExist: true,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
            GetSnapshot: getSnapshot,
        })

        node2 := NewRaftNode(&RaftNodeConfig{
            ID: 0x2,
            CreateClusterIfNotExist: false,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
            GetSnapshot: getSnapshot,
        })

        node3 := NewRaftNode(&RaftNodeConfig{
            ID: 0x3,
            CreateClusterIfNotExist: false,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
            GetSnapshot: getSnapshot,
        })

        node4 := NewRaftNode(&RaftNodeConfig{
            ID: 0x4,
            CreateClusterIfNotExist: false,
            Storage: NewRaftStorage(devicedb.NewLevelDBStorageDriver("/tmp/testraftstore-" + randomString(), nil)),
            GetSnapshot: getSnapshot,
        })

        nodeMap := map[uint64]*RaftNode{
            1: node1,
            2: node2,
            3: node3,
            4: node4,
        }

        var snapshot raftpb.Snapshot

        var run = func(id uint64, node *RaftNode) {
            for {
                select {
                case msg := <-node.Messages():
                    nodeMap[msg.To].Receive(context.TODO(), msg)
                case snap := <-node.Snapshots():
                    snapshot = snap
                case <-node.Entries():
                }
            }
        }

        go run(1, node1)
        go run(2, node2)
        go run(3, node3)
        go run(4, node4)

        Expect(node1.Start()).Should(BeNil())
        Expect(node2.Start()).Should(BeNil())
        Expect(node3.Start()).Should(BeNil())
        Expect(node4.Start()).Should(BeNil())
        <-time.After(time.Second * 1)
        Expect(node1.AddNode(context.TODO(), 2)).Should(BeNil())
        <-time.After(time.Second * 1)
        Expect(node1.AddNode(context.TODO(), 3)).Should(BeNil())
        <-time.After(time.Second * 5)

        devicedb.Log.Infof("Propose %d random entries", LogCompactionSize)

        for i := 0; i < LogCompactionSize; i++ {
            nodeMap[uint64((i % 3) + 1)].Propose(context.TODO(), []byte(randomString()))
        }

        <-time.After(time.Second * 5)

        devicedb.Log.Infof("Restart node 2")
        node2.Stop()
        <-time.After(time.Second * 1)
        Expect(node2.Start()).Should(BeNil())
        snap, _ := node2.LastSnapshot()

        Expect(snap.Metadata.ConfState.Nodes).Should(Equal([]uint64{ 1, 2, 3 }))

        <-time.After(time.Second * 5)
        devicedb.Log.Infof("Add node 4 to cluster after compactions occcurred. Should result in node 4 recieving snapshot")
        Expect(node2.AddNode(context.TODO(), 4)).Should(BeNil())
        <-time.After(time.Second * 5)
        Expect(raft.IsEmptySnap(snapshot)).Should(BeFalse())
    })
})