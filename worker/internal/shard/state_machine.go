package shard

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	sm "github.com/lni/dragonboat/v3/statemachine"
	"github.com/pavandhadge/vectron/worker/internal/storage"
)

// CommandType defines the type of command for the state machine.
type CommandType int

const (
	// StoreVector stores a vector.
	StoreVector CommandType = iota
	// DeleteVector deletes a vector.
	DeleteVector
)

// Command is a command to be applied to the state machine.
type Command struct {
	Type     CommandType
	ID       string
	Vector   []float32
	Metadata []byte
}

// SearchQuery is the query for a vector search.
type SearchQuery struct {
	Vector []float32
	K      int
}

// StateMachine is the per-shard state machine that manages a PebbleDB instance
// and an HNSW index.
type StateMachine struct {
	*storage.PebbleDB
	ClusterID uint64
	NodeID    uint64
}

// NewStateMachine creates a new shard StateMachine.
func NewStateMachine(clusterID uint64, nodeID uint64, workerDataDir string, dimension int32, distance string) (*StateMachine, error) {
	dbPath := filepath.Join(workerDataDir, fmt.Sprintf("shard-%d", clusterID))

	opts := &storage.Options{
		Path:            dbPath,
		CreateIfMissing: true,
		HNSWConfig: storage.HNSWConfig{
			Dim:            int(dimension),
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			DistanceMetric: distance,
		},
	}

	db := storage.NewPebbleDB()
	if err := db.Init(dbPath, opts); err != nil {
		return nil, fmt.Errorf("failed to initialize storage for shard %d: %w", clusterID, err)
	}

	return &StateMachine{
		PebbleDB:  db,
		ClusterID: clusterID,
		NodeID:    nodeID,
	}, nil
}

// Open is a no-op.
func (s *StateMachine) Open(stopc <-chan struct{}) (uint64, error) {
	return 0, nil
}

// Update applies commands from the Raft log to the state machine.
func (s *StateMachine) Update(entries []sm.Entry) ([]sm.Entry, error) {
	for i, entry := range entries {
		var cmd Command
		if err := json.Unmarshal(entry.Cmd, &cmd); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}

		switch cmd.Type {
		case StoreVector:
			if err := s.StoreVector(cmd.ID, cmd.Vector, cmd.Metadata); err != nil {
				return nil, fmt.Errorf("failed to store vector: %w", err)
			}
		case DeleteVector:
			if err := s.DeleteVector(cmd.ID); err != nil {
				return nil, fmt.Errorf("failed to delete vector: %w", err)
			}
		}
		entries[i].Result = sm.Result{}
	}
	return entries, nil
}

// Lookup performs a read-only query on the state machine.
func (s *StateMachine) Lookup(query interface{}) (interface{}, error) {
	switch q := query.(type) {
	case SearchQuery:
		return s.Search(q.Vector, q.K)
	default:
		return nil, fmt.Errorf("unknown query type: %T", q)
	}
}

// Sync is a no-op.
func (s *StateMachine) Sync() error {
	return nil
}

// PrepareSnapshot is a no-op.
func (s *StateMachine) PrepareSnapshot() (interface{}, error) {
	return nil, nil
}

// SaveSnapshot saves a snapshot of the state machine.
func (s *StateMachine) SaveSnapshot(ctx interface{}, w io.Writer, done <-chan struct{}) error {
	tmpDir, err := ioutil.TempDir("", "snapshot")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := s.PebbleDB.Backup(tmpDir); err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(tmpDir, path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(relPath)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fw, f)
		return err
	})
}

// RecoverFromSnapshot restores the state machine from a snapshot.
func (s *StateMachine) RecoverFromSnapshot(r io.Reader, done <-chan struct{}) error {
	// Create a temporary file to store the snapshot
	tmpFile, err := ioutil.TempFile("", "snapshot-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file for snapshot: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Copy the snapshot from the reader to the temporary file
	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("failed to write snapshot to temp file: %w", err)
	}
	tmpFile.Close() // Close the file to be able to open it with the zip reader

	// Open the zip archive for reading
	zr, err := zip.OpenReader(tmpFile.Name())
	if err != nil {
		return fmt.Errorf("failed to open snapshot zip archive: %w", err)
	}
	defer zr.Close()

	// Create a temporary directory to extract the snapshot
	tmpDir, err := ioutil.TempDir("", "snapshot-extract")
	if err != nil {
		return fmt.Errorf("failed to create temp dir for snapshot extraction: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract the files from the zip archive
	for _, f := range zr.File {
		fpath := filepath.Join(tmpDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	// Restore the database from the extracted files
	return s.PebbleDB.Restore(tmpDir)
}

// Close closes the state machine.
func (s *StateMachine) Close() error {
	return s.PebbleDB.Close()
}

// GetHash is a no-op.
func (s *StateMachine) GetHash() (uint64, error) {
	return 0, nil
}
