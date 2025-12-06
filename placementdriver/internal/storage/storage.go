package storage

import (
	"os"
	"path/filepath"

	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// Storage is a wrapper around raft-boltdb to provide a persistent store for Raft.
type Storage struct {
	*raftboltdb.BoltStore
}

// NewStorage creates a new Storage instance.
// It creates a new BoltDB file at the given path if it doesn't exist.
func NewStorage(path string) (*Storage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	store, err := raftboltdb.NewBoltStore(path)
	if err != nil {
		return nil, err
	}

	return &Storage{store}, nil
}
