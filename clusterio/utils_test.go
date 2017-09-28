package clusterio_test

import (
    "context"

    . "devicedb/bucket"
    . "devicedb/clusterio"
    . "devicedb/data"
)

type MockPartitionResolver struct {
    defaultPartitionResponse uint64
    defaultReplicaNodesResponse []uint64
    partitionCB func(partitioningKey string)
    replicaNodesCB func(partition uint64)
}

func NewMockPartitionResolver() *MockPartitionResolver {
    return &MockPartitionResolver{ }
}

func (partitionResolver *MockPartitionResolver) Partition(partitioningKey string) uint64 {
    if partitionResolver.partitionCB != nil {
        partitionResolver.partitionCB(partitioningKey)
    }

    return partitionResolver.defaultPartitionResponse
}

func (partitionResolver *MockPartitionResolver) ReplicaNodes(partition uint64) []uint64 {
    if partitionResolver.replicaNodesCB != nil {
        partitionResolver.replicaNodesCB(partition)
    }

    return partitionResolver.defaultReplicaNodesResponse
}

type MockNodeClient struct {
    defaultBatchResponse error
    defaultGetResponse []*SiblingSet
    defaultGetResponseError error
    defaultGetMatchesResponse SiblingSetIterator
    defaultGetMatchesResponseError error
    batchCB func(ctx context.Context, nodeID uint64, partition uint64, siteID string, bucket string, updateBatch *UpdateBatch) error
    getCB func(ctx context.Context, nodeID uint64, partition uint64, siteID string, bucket string, keys [][]byte) ([]*SiblingSet, error)
}

func NewMockNodeClient() *MockNodeClient {
    return &MockNodeClient{ }
}

func (nodeClient *MockNodeClient) Batch(ctx context.Context, nodeID uint64, partition uint64, siteID string, bucket string, updateBatch *UpdateBatch) error {
    if nodeClient.batchCB != nil {
        return nodeClient.batchCB(ctx, nodeID, partition, siteID, bucket, updateBatch)
    }

    return nodeClient.defaultBatchResponse
}

func (nodeClient *MockNodeClient) Get(ctx context.Context, nodeID uint64, partition uint64, siteID string, bucket string, keys [][]byte) ([]*SiblingSet, error) {
    if nodeClient.getCB != nil {
        return nodeClient.getCB(ctx, nodeID, partition, siteID, bucket, keys)
    }

    return nodeClient.defaultGetResponse, nodeClient.defaultGetResponseError
}

func (nodeClient *MockNodeClient) GetMatches(ctx context.Context, nodeID uint64, partition uint64, siteID string, bucket string, keys [][]byte) (SiblingSetIterator, error) {
    return nodeClient.defaultGetMatchesResponse, nodeClient.defaultGetMatchesResponseError
}

type MockNodeReadRepairer struct {
    beginRepairCB func(readMerger NodeReadMerger)
    stopRepairsCB func()
}

func NewMockNodeReadRepairer() *MockNodeReadRepairer {
    return &MockNodeReadRepairer{
    }
}

func (readRepairer *MockNodeReadRepairer) BeginRepair(readMerger NodeReadMerger) {
    if readRepairer.beginRepairCB != nil {
        readRepairer.beginRepairCB(readMerger)
    }
}

func (readRepairer *MockNodeReadRepairer) StopRepairs() {
    if readRepairer.stopRepairsCB != nil {
        readRepairer.stopRepairsCB()
    }
}