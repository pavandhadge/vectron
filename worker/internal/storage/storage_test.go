package storage

import (
	"os"
	"reflect"
	"testing"
)

func setupTestDB(t *testing.T) (Storage, string) {
	t.Helper()
	dbPath, err := os.MkdirTemp("", "Pebble_test_")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	db := NewPebbleDB()
	opts := &Options{CreateIfMissing: true}
	err = db.Init(dbPath, opts)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	return db, dbPath
}

func TestPutGetDelete(t *testing.T) {
	db, dbPath := setupTestDB(t)
	defer os.RemoveAll(dbPath)
	defer db.Close()

	key := []byte("hello")
	value := []byte("world")

	// Test Put
	err := db.Put(key, value)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test Get
	retrievedValue, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !reflect.DeepEqual(retrievedValue, value) {
		t.Fatalf("Get returned wrong value: got %s, want %s", retrievedValue, value)
	}

	// Test Exists
	exists, err := db.Exists(key)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatal("Exists returned false for existing key")
	}

	// Test Delete
	err = db.Delete(key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Test Get after Delete
	retrievedValue, err = db.Get(key)
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if retrievedValue != nil {
		t.Fatalf("Get after delete returned non-nil value: got %s", retrievedValue)
	}

	// Test Exists after Delete
	exists, err = db.Exists(key)
	if err != nil {
		t.Fatalf("Exists after delete failed: %v", err)
	}
	if exists {
		t.Fatal("Exists returned true for deleted key")
	}
}

func TestIteration(t *testing.T) {
	db, dbPath := setupTestDB(t)
	defer os.RemoveAll(dbPath)
	defer db.Close()

	// Add some data
	prefix := []byte("user:")
	data := []KeyValuePair{
		{Key: []byte("user:1"), Value: []byte("a")},
		{Key: []byte("user:2"), Value: []byte("b")},
		{Key: []byte("user:3"), Value: []byte("c")},
	}
	for _, kv := range data {
		err := db.Put(kv.Key, kv.Value)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}
	// Add a key that should not be included
	err := db.Put([]byte("other:1"), []byte("d"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test Scan
	scanResults, err := db.Scan(prefix, 10)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(scanResults) != len(data) {
		t.Fatalf("Scan returned wrong number of results: got %d, want %d", len(scanResults), len(data))
	}
	for i, kv := range scanResults {
		if !reflect.DeepEqual(kv.Key, data[i].Key) {
			t.Errorf("Scan returned wrong key at index %d: got %s, want %s", i, kv.Key, data[i].Key)
		}
	}

	// Test Iterate
	var iterateResults []KeyValuePair
	err = db.Iterate(prefix, func(key, value []byte) bool {
		iterateResults = append(iterateResults, KeyValuePair{Key: key, Value: value})
		return true
	})
	if err != nil {
		t.Fatalf("Iterate failed: %v", err)
	}
	if len(iterateResults) != len(data) {
		t.Fatalf("Iterate returned wrong number of results: got %d, want %d", len(iterateResults), len(data))
	}
}

func TestBatch(t *testing.T) {
	db, dbPath := setupTestDB(t)
	defer os.RemoveAll(dbPath)
	defer db.Close()

	puts := map[string][]byte{
		"key1": []byte("val1"),
		"key2": []byte("val2"),
	}

	err := db.BatchPut(puts)
	if err != nil {
		t.Fatalf("BatchPut failed: %v", err)
	}

	for k, v := range puts {
		retrieved, err := db.Get([]byte(k))
		if err != nil {
			t.Fatalf("Get failed for key %s: %v", k, err)
		}
		if !reflect.DeepEqual(retrieved, v) {
			t.Fatalf("Get returned wrong value for key %s: got %s, want %s", k, retrieved, v)
		}
	}

	deletes := []string{"key1", "key2"}
	err = db.BatchDelete(deletes)
	if err != nil {
		t.Fatalf("BatchDelete failed: %v", err)
	}

	for _, k := range deletes {
		exists, err := db.Exists([]byte(k))
		if err != nil {
			t.Fatalf("Exists failed for key %s: %v", k, err)
		}
		if exists {
			t.Fatalf("Key %s should have been deleted", k)
		}
	}
}
