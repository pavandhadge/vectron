package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

// server implements the PlacementService
type server struct {
	pb.UnimplementedPlacementServiceServer

	mu              sync.RWMutex
	workerStore     WorkerStore
	collectionStore CollectionStore
	workerIdx       int // for round-robin
}

type WorkerInfo struct {
	ID            string
	Address       string
	LastHeartbeat time.Time
	Collections   []string
}

type CollectionInfo struct {
	Name     string
	WorkerID string
}

type WorkerStore interface {
	RegisterWorker(ctx context.Context, workerInfo WorkerInfo) error
	GetWorker(ctx context.Context, workerID string) (WorkerInfo, error)
	ListWorkers(ctx context.Context) ([]WorkerInfo, error)
	DeleteWorker(ctx context.Context, workerID string) error
	UpdateWorkerHeartbeat(ctx context.Context, workerID string, heartbeat time.Time) error
}

type CollectionStore interface {
	CreateCollection(ctx context.Context, collectionInfo CollectionInfo) error
	GetCollection(ctx context.Context, collectionName string) (CollectionInfo, error)
	ListCollections(ctx context.Context) ([]CollectionInfo, error)
	DeleteCollection(ctx context.Context, collectionName string) error
}

type etcdStore struct {
	client *clientv3.Client
}

func NewEtcdStore(client *clientv3.Client) (*etcdStore, error) {
	return &etcdStore{client: client}, nil
}

func (s *etcdStore) RegisterWorker(ctx context.Context, workerInfo WorkerInfo) error {
	key := fmt.Sprintf("/workers/%s", workerInfo.ID)
	value, err := json.Marshal(workerInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal worker info: %w", err)
	}

	_, err = s.client.Put(ctx, key, string(value))
	if err != nil {
		return fmt.Errorf("failed to put worker info into etcd: %w", err)
	}

	return nil
}

func (s *etcdStore) GetWorker(ctx context.Context, workerID string) (WorkerInfo, error) {
	key := fmt.Sprintf("/workers/%s", workerID)

	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return WorkerInfo{}, fmt.Errorf("failed to get worker info from etcd: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return WorkerInfo{}, fmt.Errorf("worker %s not found", workerID)
	}

	var workerInfo WorkerInfo
	err = json.Unmarshal(resp.Kvs[0].Value, &workerInfo)
	if err != nil {
		return WorkerInfo{}, fmt.Errorf("failed to unmarshal worker info: %w", err)
	}

	return workerInfo, nil
}

func (s *etcdStore) ListWorkers(ctx context.Context) ([]WorkerInfo, error) {
	resp, err := s.client.Get(ctx, "/workers/", clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list workers from etcd: %w", err)
	}

	var workerInfos []WorkerInfo
	for _, kv := range resp.Kvs {
		var workerInfo WorkerInfo
		err = json.Unmarshal(kv.Value, &workerInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal worker info: %w", err)
		}
		workerInfos = append(workerInfos, workerInfo)
	}

	return workerInfos, nil
}

func (s *etcdStore) DeleteWorker(ctx context.Context, workerID string) error {
	key := fmt.Sprintf("/workers/%s", workerID)

	_, err := s.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete worker from etcd: %w", err)
	}

	return nil
}

func (s *etcdStore) UpdateWorkerHeartbeat(ctx context.Context, workerID string, heartbeat time.Time) error {
	workerInfo, err := s.GetWorker(ctx, workerID)
	if err != nil {
		return err
	}
	workerInfo.LastHeartbeat = heartbeat
	return s.RegisterWorker(ctx, workerInfo)
}

func (s *etcdStore) CreateCollection(ctx context.Context, collectionInfo CollectionInfo) error {
	key := fmt.Sprintf("/collections/%s", collectionInfo.Name)
	value, err := json.Marshal(collectionInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal collection info: %w", err)
	}

	_, err = s.client.Put(ctx, key, string(value))
	if err != nil {
		return fmt.Errorf("failed to put collection info into etcd: %w", err)
	}

	return nil
}

func (s *etcdStore) GetCollection(ctx context.Context, collectionName string) (CollectionInfo, error) {
	key := fmt.Sprintf("/collections/%s", collectionName)

	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("failed to get collection info from etcd: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return CollectionInfo{}, fmt.Errorf("collection %s not found", collectionName)
	}

	var collectionInfo CollectionInfo
	err = json.Unmarshal(resp.Kvs[0].Value, &collectionInfo)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("failed to unmarshal collection info: %w", err)
	}

	return collectionInfo, nil
}

func (s *etcdStore) ListCollections(ctx context.Context) ([]CollectionInfo, error) {
	resp, err := s.client.Get(ctx, "/collections/", clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list collections from etcd: %w", err)
	}

	var collectionInfos []CollectionInfo
	for _, kv := range resp.Kvs {
		var collectionInfo CollectionInfo
		err = json.Unmarshal(kv.Value, &collectionInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal collection info: %w", err)
		}
		collectionInfos = append(collectionInfos, collectionInfo)
	}

	return collectionInfos, nil
}

func (s *etcdStore) DeleteCollection(ctx context.Context, collectionName string) error {
	key := fmt.Sprintf("/collections/%s", collectionName)

	_, err := s.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete collection from etcd: %w", err)
	}

	return nil
}

var heartbeatTimeout *time.Duration

func main() {
	// You can pass the port via command-line flag or env var in a real app
	port := "6001"

	heartbeatTimeout = flag.Duration("heartbeat-timeout", 30*time.Second, "heartbeat timeout")
	flag.Parse()

	// Initialize etcd client
	etcdEndpoints := []string{"localhost:2379"} // Replace with your etcd endpoints
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close()

	etcdStore, err := NewEtcdStore(etcdClient)
	if err != nil {
		log.Fatalf("failed to create etcd store: %v", err)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPlacementServiceServer(grpcServer, &server{
		workerStore:     etcdStore,
		collectionStore: etcdStore,
	})

	log.Printf("Placement driver listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC server: %v", err)
	}
}
