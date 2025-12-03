package storageTest

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	Storage "github.com/pavandhadge/vectron/worker/internal/storage"
)

func setupTestDB(t *testing.T) (Storage.Storage, string) {
	t.Helper()
	dbPath, err := os.MkdirTemp("", "Pebble_test_")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	db := Storage.NewPebbleDB()
	opts := &Storage.Options{CreateIfMissing: true}
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

	prefix := []byte("user:")

	// Intentionally NOT in sorted order — to catch ordering bugs
	data := []Storage.KeyValuePair{
		{Key: []byte("user:10"), Value: []byte("john")},
		{Key: []byte("user:2"), Value: []byte("bob")},
		{Key: []byte("user:1"), Value: []byte("alice")},
		{Key: []byte("user:30"), Value: []byte("charlie")},
	}

	expected := make(map[string][]byte)
	for _, kv := range data {
		if err := db.Put(kv.Key, kv.Value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		expected[string(kv.Key)] = kv.Value
	}

	// This key must NOT appear
	if err := db.Put([]byte("other:1"), []byte("d")); err != nil {
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

	// Order-independent + safe comparison
	for _, kv := range scanResults {
		want, ok := expected[string(kv.Key)]
		if !ok {
			t.Errorf("Scan returned unexpected key: %s", kv.Key)
			continue
		}
		if !bytes.Equal(kv.Value, want) {
			t.Errorf("wrong value for key %s: got %s, want %s", kv.Key, kv.Value, want)
		}
		delete(expected, string(kv.Key))
	}
	if len(expected) > 0 {
		t.Errorf("Scan missing keys: %v", expected)
	}

	// Test Iterate
	var iterateResults []Storage.KeyValuePair
	err = db.Iterate(prefix, func(key, value []byte) bool {
		// MUST copy — Pebble reuses buffers!
		iterateResults = append(iterateResults, Storage.KeyValuePair{
			Key:   append([]byte(nil), key...),
			Value: append([]byte(nil), value...),
		})
		return true
	})
	if err != nil {
		t.Fatalf("Iterate failed: %v", err)
	}

	if len(iterateResults) != len(data) {
		t.Fatalf("Iterate returned wrong number of results: got %d, want %d", len(iterateResults), len(data))
	}

	// Optional: verify keys are sorted (Pebble guarantees this)
	for i := 1; i < len(iterateResults); i++ {
		if bytes.Compare(iterateResults[i-1].Key, iterateResults[i].Key) > 0 {
			t.Errorf("Iterate not returning keys in sorted order: %s > %s",
				iterateResults[i-1].Key, iterateResults[i].Key)
		}
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
