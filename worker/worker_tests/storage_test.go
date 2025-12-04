package worker_tests

import (
	"os"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/pavandhadge/vectron/worker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDeleteVector_SoftDeleteBehavior(t *testing.T) {
	db, teardown := setupTestDB(t, false)
	defer teardown()

	id := "123456789"
	vector := []float32{0.1, 0.2, 0.3, 0.4}
	metadata := []byte("test_metadata")

	// 1. Store vector
	require.NoError(t, db.StoreVector(id, vector, metadata), "failed to store vector")

	// 2. Delete it
	require.NoError(t, db.DeleteVector(id), "failed to delete vector")

	// 3. Public API: GetVector should behave exactly like "not found"
	vec, meta, err := db.GetVector(id)
	require.NoError(t, err) // No error!
	require.Nil(t, vec)     // Vector is nil
	require.Nil(t, meta)    // Metadata is nil (or empty)

	// 4. Confirm it was actually soft-deleted (testing/admin API)
	deleted, err := db.IsDeleted(id)
	require.NoError(t, err)
	require.True(t, deleted, "vector should be marked as soft-deleted in storage")

	// 5. Optional: confirm key still exists in DB (tombstone present)
	existsInStorage, err := db.Exists([]byte(id))
	require.NoError(t, err)
	require.False(t, existsInStorage, "key should be GONE from storage after delete")
	// 6. Delete again → should be idempotent
	require.NoError(t, db.DeleteVector(id), "second delete should not fail")
}

func TestSearch(t *testing.T) {
	db, teardown := setupTestDB(t, false)
	defer teardown()

	vectors := map[string][]float32{
		"1234561": {0.1, 0.1, 0.1, 0.1},
		"1234562": {0.2, 0.2, 0.2, 0.2},
		"1234563": {0.8, 0.8, 0.8, 0.8},
		"1234564": {0.9, 0.9, 0.9, 0.9},
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

	expected := []string{"1234561", "1234562"}
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
		"1234561": {0.1, 0.1, 0.1, 0.1},
		"1234562": {0.2, 0.2, 0.2, 0.2},
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

	expected := []string{"1234561", "1234562"}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("expected search results %v, got %v", expected, results)
	}
}

func TestDeleteVector(t *testing.T) {
	db, cleanup := setupTestDB(t, true)
	defer cleanup()

	id := "1234561"
	vec := []float32{1.0, 2.0, 1.0, 2.0}
	meta := []byte("test_metadata")

	err := db.StoreVector(id, vec, meta)
	assert.NoError(t, err)

	err = db.DeleteVector(id)
	assert.NoError(t, err)

	retrievedVec, _, err := db.GetVector(id)
	assert.NoError(t, err)
	assert.Nil(t, retrievedVec)
}

func TestGetVectorNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t, true)
	defer cleanup()

	retrievedVec, _, err := db.GetVector("non_existent_vector")
	assert.NoError(t, err)
	assert.Nil(t, retrievedVec)
}

func TestBruteForceSearch(t *testing.T) {
	db, cleanup := setupTestDB(t, true)
	defer cleanup()

	vecs := map[string][]float32{
		"1234561712": {4.0, 1.0, 1.0, 2.0},
		"1234562812": {4.1, 1.1, 1.0, 2.0},
		"1234563512": {5.0, 5.0, 1.0, 2.0},
		"1234564512": {5.1, 5.1, 1.0, 2.0},
	}

	for id, vec := range vecs {
		err := db.StoreVector(id, vec, nil)
		assert.NoError(t, err)
	}

	query := []float32{4.0, 1.0, 1.0, 2.0}
	k := 2
	results, err := db.BruteForceSearch(query, k)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Contains(t, results, "1234561712")
	assert.Contains(t, results, "1234562812")
}

func TestWAL_CrashRecovery(t *testing.T) {
	dir, err := os.MkdirTemp("", "pebble_wal_test")
	require.NoError(t, err)

	opts := &storage.Options{
		HNSWConfig: storage.HNSWConfig{
			Dim:            4,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			WALEnabled:     true,
		},
	}

	// === PHASE 1: Simulate normal operation + crash ===
	{
		db := storage.NewPebbleDB()
		require.NoError(t, db.Init(dir, opts))

		vectors := map[string][]float32{
			"1234561": {0.1, 0.1, 0.1, 0.1},
			"1234562": {0.2, 0.2, 0.2, 0.2},
		}

		for id, vec := range vectors {
			require.NoError(t, db.StoreVector(id, vec, nil))
		}

		// Force WAL to disk
		require.NoError(t, db.Flush())
		// require.NoError(t, db.)

		// CRITICAL: Close the DB properly but DO NOT delete the dir
		require.NoError(t, db.Close())

		// Force garbage collection so locks are released
		runtime.GC()
		runtime.Gosched()
	}

	// === PHASE 2: Simulate process restart (new process opens same dir) ===
	db2 := storage.NewPebbleDB()
	require.NoError(t, db2.Init(dir, opts))
	defer db2.Close()

	// Now WAL should be replayed
	query := []float32{0.0, 0.0, 0.0, 0.0}
	results, err := db2.Search(query, 2)
	require.NoError(t, err)

	// Sort because HNSW order is non-deterministic
	sort.Strings(results)
	expected := []string{"1234561", "1234562"}
	sort.Strings(expected)

	if !reflect.DeepEqual(results, expected) {
		t.Errorf("WAL recovery failed: expected %v, got %v", expected, results)
	}

	// Final cleanup
	os.RemoveAll(dir)
}
