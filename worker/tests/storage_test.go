package tests

import (
	"os"
	"reflect"
	"testing"

	"github.com/pavandhadge/vectron/worker/internal/storage"
)

func setupTestDB(t *testing.T, walEnabled bool) (*storage.PebbleDB, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "pebble_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	opts := &storage.Options{
		HNSWConfig: storage.HNSWConfig{
			Dim:            4,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			WALEnabled:     walEnabled,
		},
	}

	db := storage.NewPebbleDB()
	if err := db.Init(dir, opts); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	teardown := func() {
		db.Close()
		os.RemoveAll(dir)
	}

	return db, teardown
}

func TestStoreAndGetVector(t *testing.T) {
	db, teardown := setupTestDB(t, false)
	defer teardown()

	id := "test_vector"
	vector := []float32{0.1, 0.2, 0.3, 0.4}
	metadata := []byte("test_metadata")

	if err := db.StoreVector(id, vector, metadata); err != nil {
		t.Fatalf("failed to store vector: %v", err)
	}

	retrievedVector, retrievedMetadata, err := db.GetVector(id)
	if err != nil {
		t.Fatalf("failed to get vector: %v", err)
	}

	if !reflect.DeepEqual(vector, retrievedVector) {
		t.Errorf("expected vector %v, got %v", vector, retrievedVector)
	}

	if !reflect.DeepEqual(metadata, retrievedMetadata) {
		t.Errorf("expected metadata %v, got %v", metadata, retrievedMetadata)
	}
}

func TestDeleteVector(t *testing.T) {
	db, teardown := setupTestDB(t, false)
	defer teardown()

	id := "test_vector"
	vector := []float32{0.1, 0.2, 0.3, 0.4}
	metadata := []byte("test_metadata")

	if err := db.StoreVector(id, vector, metadata); err != nil {
		t.Fatalf("failed to store vector: %v", err)
	}

	if err := db.DeleteVector(id); err != nil {
		t.Fatalf("failed to delete vector: %v", err)
	}

	_, _, err := db.GetVector(id)
	if err == nil {
		t.Errorf("expected error when getting deleted vector, but got nil")
	}
}

func TestSearch(t *testing.T) {
	db, teardown := setupTestDB(t, false)
	defer teardown()

	vectors := map[string][]float32{
		"v1": {0.1, 0.1, 0.1, 0.1},
		"v2": {0.2, 0.2, 0.2, 0.2},
		"v3": {0.8, 0.8, 0.8, 0.8},
		"v4": {0.9, 0.9, 0.9, 0.9},
	}

	for id, vec := range vectors {
		if err := db.StoreVector(id, vec, nil); err != nil {
			t.Fatalf("failed to store vector: %v", err)
		}
	}

	query := []float32{0.0, 0.0, 0.0, 0.0}
	results, err := db.Search(query, 2)
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}

	expected := []string{"v1", "v2"}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("expected search results %v, got %v", expected, results)
	}
}

func TestPersistence(t *testing.T) {
	dir, err := os.MkdirTemp("", "pebble_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	opts := &storage.Options{
		HNSWConfig: storage.HNSWConfig{
			Dim:            4,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			WALEnabled:     false,
		},
	}

	// First run
	db := storage.NewPebbleDB()
	if err := db.Init(dir, opts); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	vectors := map[string][]float32{
		"v1": {0.1, 0.1, 0.1, 0.1},
		"v2": {0.2, 0.2, 0.2, 0.2},
	}

	for id, vec := range vectors {
		if err := db.StoreVector(id, vec, nil); err != nil {
			t.Fatalf("failed to store vector: %v", err)
		}
	}
	db.Close()

	// Second run
	db2 := storage.NewPebbleDB()
	if err := db2.Init(dir, opts); err != nil {
		t.Fatalf("failed to init db on second run: %v", err)
	}
	defer db2.Close()

	query := []float32{0.0, 0.0, 0.0, 0.0}
	results, err := db2.Search(query, 2)
	if err != nil {
		t.Fatalf("failed to search on second run: %v", err)
	}

	expected := []string{"v1", "v2"}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("expected search results %v, got %v", expected, results)
	}
}

func TestWAL(t *testing.T) {
	dir, err := os.MkdirTemp("", "pebble_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	opts := &storage.Options{
		HNSWConfig: storage.HNSWConfig{
			Dim:            4,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			WALEnabled:     true,
		},
	}

	// First run with "crash"
	db := storage.NewPebbleDB()
	if err := db.Init(dir, opts); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	vectors := map[string][]float32{
		"v1": {0.1, 0.1, 0.1, 0.1},
		"v2": {0.2, 0.2, 0.2, 0.2},
	}

	for id, vec := range vectors {
		if err := db.StoreVector(id, vec, nil); err != nil {
			t.Fatalf("failed to store vector: %v", err)
		}
	}
	// Don't call db.Close() to simulate a crash

	// Second run
	db2 := storage.NewPebbleDB()
	if err := db2.Init(dir, opts); err != nil {
		t.Fatalf("failed to init db on second run: %v", err)
	}
	defer db2.Close()

	query := []float32{0.0, 0.0, 0.0, 0.0}
	results, err := db2.Search(query, 2)
	if err != nil {
		t.Fatalf("failed to search on second run: %v", err)
	}

	expected := []string{"v1", "v2"}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("expected search results %v, got %v", expected, results)
	}
}
