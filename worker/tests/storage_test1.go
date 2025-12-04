package tests

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupTestDB(t *testing.T) (*PebbleDB, func()) {
	path := "./test_db"
	db := NewPebbleDB()
	opts := &Options{
		Path: path,
		HNSWConfig: HNSWConfig{
			Dim:            2,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			DistanceMetric: "euclidean",
			WALEnabled:     false,
		},
	}
	err := db.Init(opts.Path, opts)
	assert.NoError(t, err)

	cleanup := func() {
		db.Close()
		os.RemoveAll(path)
	}

	return db, cleanup
}

func TestStoreAndGetVector(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id := "test_vector"
	vec := []float32{1.0, 2.0}
	meta := []byte("test_metadata")

	err := db.StoreVector(id, vec, meta)
	assert.NoError(t, err)

	retrievedVec, retrievedMeta, err := db.GetVector(id)
	assert.NoError(t, err)
	assert.Equal(t, vec, retrievedVec)
	assert.Equal(t, meta, retrievedMeta)
}

func TestDeleteVector(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id := "test_vector"
	vec := []float32{1.0, 2.0}
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
	db, cleanup := setupTestDB(t)
	defer cleanup()

	retrievedVec, _, err := db.GetVector("non_existent_vector")
	assert.NoError(t, err)
	assert.Nil(t, retrievedVec)
}

func TestSearch(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	vecs := map[string][]float32{
		"1234561": {1.0, 1.0},
		"1234562": {1.1, 1.1},
		"1234563": {5.0, 5.0},
		"1234564": {5.1, 5.1},
	}

	for id, vec := range vecs {
		err := db.StoreVector(id, vec, nil)
		assert.NoError(t, err)
	}

	query := []float32{1.0, 1.0}
	k := 2
	results, err := db.Search(query, k)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Contains(t, results, "1234561")
	assert.Contains(t, results, "1234562")
}

func TestBruteForceSearch(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	vecs := map[string][]float32{
		"1234561": {1.0, 1.0},
		"1234562": {1.1, 1.1},
		"1234563": {5.0, 5.0},
		"1234564": {5.1, 5.1},
	}

	for id, vec := range vecs {
		err := db.StoreVector(id, vec, nil)
		assert.NoError(t, err)
	}

	query := []float32{1.0, 1.0}
	k := 2
	results, err := db.BruteForceSearch(query, k)
	assert.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Contains(t, results, "1234561")
	assert.Contains(t, results, "1234562")
}
