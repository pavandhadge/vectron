package shard

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

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
	// StoreVectorBatch stores multiple vectors.
	StoreVectorBatch
)

// Command is a command to be applied to the state machine.
type Command struct {
	Type     CommandType
	ID       string
	Vector   []float32
	Metadata []byte
	Vectors  []VectorEntry
}

const (
	commandEncodingGob    byte = 1
	commandEncodingBinary byte = 2
)

// EncodeCommand serializes a command to bytes using a versioned binary format.
func EncodeCommand(cmd Command) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(commandEncodingBinary)
	if err := writeUint32(&buf, uint32(cmd.Type)); err != nil {
		return nil, err
	}
	if err := writeString(&buf, cmd.ID); err != nil {
		return nil, err
	}
	if err := writeFloat32Slice(&buf, cmd.Vector); err != nil {
		return nil, err
	}
	if err := writeBytes(&buf, cmd.Metadata); err != nil {
		return nil, err
	}
	if err := writeUint32(&buf, uint32(len(cmd.Vectors))); err != nil {
		return nil, err
	}
	for _, v := range cmd.Vectors {
		if err := writeString(&buf, v.ID); err != nil {
			return nil, err
		}
		if err := writeFloat32Slice(&buf, v.Vector); err != nil {
			return nil, err
		}
		if err := writeBytes(&buf, v.Metadata); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// DecodeCommand deserializes a command from bytes.
// It supports legacy JSON-encoded commands for backward compatibility.
func DecodeCommand(data []byte) (Command, error) {
	var cmd Command
	if len(data) == 0 {
		return cmd, fmt.Errorf("empty command payload")
	}
	if data[0] == '{' {
		if err := json.Unmarshal(data, &cmd); err != nil {
			return cmd, err
		}
		return cmd, nil
	}
	switch data[0] {
	case commandEncodingGob:
		if err := gob.NewDecoder(bytes.NewReader(data[1:])).Decode(&cmd); err != nil {
			return cmd, err
		}
		return cmd, nil
	case commandEncodingBinary:
		dec := bytes.NewReader(data[1:])
		typ, err := readUint32(dec)
		if err != nil {
			return cmd, err
		}
		cmd.Type = CommandType(typ)
		cmd.ID, err = readString(dec)
		if err != nil {
			return cmd, err
		}
		cmd.Vector, err = readFloat32Slice(dec)
		if err != nil {
			return cmd, err
		}
		cmd.Metadata, err = readBytes(dec)
		if err != nil {
			return cmd, err
		}
		vecCount, err := readUint32(dec)
		if err != nil {
			return cmd, err
		}
		if vecCount > 0 {
			cmd.Vectors = make([]VectorEntry, 0, vecCount)
			for i := uint32(0); i < vecCount; i++ {
				id, err := readString(dec)
				if err != nil {
					return cmd, err
				}
				vec, err := readFloat32Slice(dec)
				if err != nil {
					return cmd, err
				}
				meta, err := readBytes(dec)
				if err != nil {
					return cmd, err
				}
				cmd.Vectors = append(cmd.Vectors, VectorEntry{
					ID:       id,
					Vector:   vec,
					Metadata: meta,
				})
			}
		}
		return cmd, nil
	default:
		return cmd, fmt.Errorf("unknown command encoding version: %d", data[0])
	}
}

func writeUint32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func readUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func writeString(w io.Writer, s string) error {
	if err := writeUint32(w, uint32(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func readString(r io.Reader) (string, error) {
	n, err := readUint32(r)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func writeBytes(w io.Writer, b []byte) error {
	if err := writeUint32(w, uint32(len(b))); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func readBytes(r io.Reader) ([]byte, error) {
	n, err := readUint32(r)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFloat32Slice(w io.Writer, vals []float32) error {
	if err := writeUint32(w, uint32(len(vals))); err != nil {
		return err
	}
	var buf [4]byte
	for _, v := range vals {
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
		if _, err := w.Write(buf[:]); err != nil {
			return err
		}
	}
	return nil
}

func readFloat32Slice(r io.Reader) ([]float32, error) {
	n, err := readUint32(r)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]float32, n)
	var buf [4]byte
	for i := uint32(0); i < n; i++ {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, err
		}
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[:]))
	}
	return out, nil
}

// VectorEntry represents a single vector payload for batch commands.
type VectorEntry struct {
	ID       string
	Vector   []float32
	Metadata []byte
}

// SearchQuery is the query for a vector search.
type SearchQuery struct {
	Vector []float32
	K      int
}

type GetVectorQuery struct {
	ID string
}

type GetVectorQueryResult struct {
	Vector   []float32
	Metadata []byte
}

type snapshotContext struct {
	LastApplied uint64
}

const lastAppliedSnapshotFile = "_raft_last_applied"

// StateMachine is the per-shard state machine that manages a PebbleDB instance
// and an HNSW index.
type StateMachine struct {
	*storage.PebbleDB
	ClusterID uint64
	NodeID    uint64
	lastApplied uint64
}

func writeLastApplied(dir string, index uint64) error {
	path := filepath.Join(dir, lastAppliedSnapshotFile)
	return os.WriteFile(path, []byte(strconv.FormatUint(index, 10)), 0644)
}

func readLastApplied(dir string) (uint64, error) {
	path := filepath.Join(dir, lastAppliedSnapshotFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(bytes.TrimSpace(data)), 10, 64)
}

// NewStateMachine creates a new shard StateMachine.
func NewStateMachine(clusterID uint64, nodeID uint64, workerDataDir string, dimension int32, distance string) (*StateMachine, error) {
	dbPath := filepath.Join(workerDataDir, fmt.Sprintf("shard-%d", clusterID))

	opts := &storage.Options{
		Path:            dbPath,
		CreateIfMissing: true,
		HNSWConfig: storage.HNSWConfig{
			Dim:              int(dimension),
			M:                16,
			EfConstruction:   200,
			EfSearch:         100, // check what it controlls then tunr it
			DistanceMetric:   distance,
			NormalizeVectors: distance == "cosine",
			QuantizeVectors:  distance == "cosine",
			VectorCompressionEnabled: distance == "cosine",
			MultiStageEnabled:        true,
			MaintenanceEnabled: true,
			MaintenanceInterval: 30 * time.Minute,
			PruneEnabled:      true,
			PruneMaxNodes:     2000,
			MmapVectorsEnabled: true,
			AsyncIndexingEnabled:  true,
			IndexingQueueSize:     20000,
			IndexingBatchSize:     512,
			IndexingFlushInterval: 5 * time.Millisecond,
			WarmupEnabled:         true,
			WarmupMaxVectors:      10000,
			WarmupDelay:           5 * time.Second,
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
	last := atomic.LoadUint64(&s.lastApplied)
	if last > 0 {
		setAppliedIndex(s.ClusterID, last)
	}
	return last, nil
}

// Update applies commands from the Raft log to the state machine.
func (s *StateMachine) Update(entries []sm.Entry) ([]sm.Entry, error) {
	for i, entry := range entries {
		cmd, err := DecodeCommand(entry.Cmd)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}

		switch cmd.Type {
		case StoreVector:
			if err := s.StoreVector(cmd.ID, cmd.Vector, cmd.Metadata); err != nil {
				return nil, fmt.Errorf("failed to store vector: %w", err)
			}
		case StoreVectorBatch:
			batch := make([]storage.VectorEntry, 0, len(cmd.Vectors))
			for _, v := range cmd.Vectors {
				batch = append(batch, storage.VectorEntry{
					ID:       v.ID,
					Vector:   v.Vector,
					Metadata: v.Metadata,
				})
			}
			if err := s.StoreVectorBatch(batch); err != nil {
				return nil, fmt.Errorf("failed to store vector batch: %w", err)
			}
		case DeleteVector:
			if err := s.DeleteVector(cmd.ID); err != nil {
				return nil, fmt.Errorf("failed to delete vector: %w", err)
			}
		}
		entries[i].Result = sm.Result{}
	}
	if len(entries) > 0 {
		last := entries[len(entries)-1].Index
		atomic.StoreUint64(&s.lastApplied, last)
		setAppliedIndex(s.ClusterID, last)
	}
	return entries, nil
}

type SearchResult struct {
	IDs    []string
	Scores []float32
}

// Lookup performs a read-only query on the state machine.
func (s *StateMachine) Lookup(query interface{}) (interface{}, error) {
	switch q := query.(type) {
	case SearchQuery:
		ids, scores, err := s.Search(q.Vector, q.K)
		if err != nil {
			return nil, err
		}
		return &SearchResult{IDs: ids, Scores: scores}, nil
	case GetVectorQuery:
		vec, meta, err := s.GetVector(q.ID)
		if err != nil {
			return nil, err
		}
		if vec == nil {
			return nil, nil // Not found
		}
		return &GetVectorQueryResult{
			Vector:   vec,
			Metadata: meta,
		}, nil
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
	return snapshotContext{LastApplied: atomic.LoadUint64(&s.lastApplied)}, nil
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

	if snapCtx, ok := ctx.(snapshotContext); ok && snapCtx.LastApplied > 0 {
		if err := writeLastApplied(tmpDir, snapCtx.LastApplied); err != nil {
			return err
		}
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
	if err := s.PebbleDB.Restore(tmpDir); err != nil {
		return err
	}

	if lastApplied, err := readLastApplied(tmpDir); err == nil && lastApplied > 0 {
		atomic.StoreUint64(&s.lastApplied, lastApplied)
		setAppliedIndex(s.ClusterID, lastApplied)
	}
	return nil
}

// Close closes the state machine.
func (s *StateMachine) Close() error {
	return s.PebbleDB.Close()
}

// GetHash is a no-op.
func (s *StateMachine) GetHash() (uint64, error) {
	return 0, nil
}
