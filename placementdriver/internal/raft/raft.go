package raft

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	"github.com/pavandhadge/vectron/placementdriver/internal/storage"
)

const (
	retainSnapshotCount = 2
	raftTimeout         = 10 * time.Second
)

// Raft is a wrapper around the HashiCorp Raft implementation.
type Raft struct {
	*raft.Raft
}

// Config contains the configuration for the Raft node.
type Config struct {
	NodeID     string
	ListenAddr string
	DataDir    string
	Peers      []string
	Bootstrap  bool
}

// NewRaft creates a new Raft node.
func NewRaft(config *Config, fsm *fsm.FSM) (*Raft, error) {
	// Setup Raft configuration.
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)

	// Setup Raft communication.
	addr, err := net.ResolveTCPAddr("tcp", config.ListenAddr)
	if err != nil {
		return nil, err
	}
	transport, err := raft.NewTCPTransport(config.ListenAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, err
	}

	// Create the snapshot store. This allows Raft to truncate the log.
	snapshots, err := raft.NewFileSnapshotStore(config.DataDir, retainSnapshotCount, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("file snapshot store: %s", err)
	}

	// Create the log store and stable store.
	logStore, err := storage.NewStorage(filepath.Join(config.DataDir, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("new bolt store: %s", err)
	}

	// Instantiate the Raft systems.
	ra, err := raft.NewRaft(raftConfig, fsm, logStore, logStore, snapshots, transport)
	if err != nil {
		return nil, fmt.Errorf("new raft: %s", err)
	}

	// If bootstrap is enabled, and there are no existing peers, bootstrap the cluster.
	if config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(config.NodeID),
					Address: transport.LocalAddr(),
				},
			},
		}
		ra.BootstrapCluster(configuration)
	}

	return &Raft{ra}, nil
}

// Propose proposes a command to the Raft cluster.
func (r *Raft) Propose(cmd []byte) error {
	future := r.Apply(cmd, raftTimeout)
	if err := future.Error(); err != nil {
		return err
	}

	if err, ok := future.Response().(error); ok {
		return err
	}

	return nil
}
