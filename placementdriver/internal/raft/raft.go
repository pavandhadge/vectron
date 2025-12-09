package raft

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	sm "github.com/lni/dragonboat/v3/statemachine"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
)

// Config contains the configuration for the Dragonboat node.
type Config struct {
	// NodeID is the unique ID of the node in the cluster. It must be > 0.
	NodeID uint64
	// ClusterID is the unique ID of the raft cluster.
	ClusterID uint64
	// RaftAddress is the address for raft communication, e.g. "localhost:7001".
	RaftAddress string
	// InitialMembers is a map of node IDs to their raft addresses.
	// Used for bootstrapping the cluster.
	InitialMembers map[uint64]string
	// DataDir is the directory for storing raft logs and snapshots.
	DataDir string
}

// Node is a wrapper around the Dragonboat NodeHost.
type Node struct {
	*dragonboat.NodeHost
	config Config
	fsm    *fsm.FSM
}

// NewNode creates and starts a new Dragonboat Raft node.
func NewNode(cfg Config) (*Node, error) {
	var fsmInstance *fsm.FSM
	// Create the directory for NodeHost data.
	nhDataDir := filepath.Join(cfg.DataDir, fmt.Sprintf("nodehost-%d", cfg.NodeID))

	// Configure the NodeHost.
	nhc := config.NodeHostConfig{
		DeploymentID:   1, // A unique ID for the deployment.
		NodeHostDir:    nhDataDir,
		RaftAddress:    cfg.RaftAddress,
		ListenAddress:  cfg.RaftAddress, // Use the same for simplicity.
		EnableMetrics:  true,
		RTTMillisecond: 200,
		// RaftEventListener: &loggingEventListener{}, // Add a simple logger.
	}

	// Create the NodeHost.
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		return nil, fmt.Errorf("failed to create nodehost: %w", err)
	}

	// Configure the Raft cluster.
	rc := config.Config{
		NodeID:             cfg.NodeID,
		ClusterID:          cfg.ClusterID,
		ElectionRTT:        5,
		HeartbeatRTT:       1,
		CheckQuorum:        true,
		SnapshotEntries:    1000,
		CompactionOverhead: 500,
	}

	// Define the state machine factory function.
	createFSM := func(clusterID uint64, nodeID uint64) sm.IOnDiskStateMachine {
		fsmInstance = fsm.NewFSM()
		return fsmInstance
	}

	// Start the Raft cluster.
	if err := nh.StartOnDiskCluster(cfg.InitialMembers, false, createFSM, rc); err != nil {
		return nil, fmt.Errorf("failed to start cluster: %w", err)
	}

	return &Node{
			NodeHost: nh,
			config:   cfg,
			fsm:      fsmInstance,
		},
		nil
}

// GetFSM returns a pointer to the FSM instance.
func (n *Node) GetFSM() *fsm.FSM {
	return n.fsm
}

// Propose submits a command to the Raft cluster and waits for it to be committed.

func (n *Node) Propose(cmd []byte, timeout time.Duration) (sm.Result, error) {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	defer cancel()

	cs := n.GetNoOPSession(n.config.ClusterID)

	res, err := n.NodeHost.SyncPropose(ctx, cs, cmd)

	if err != nil {

		return sm.Result{}, fmt.Errorf("failed to propose command: %w", err)

	}

	return res, nil

}

// Read performs a linearizable read from the state machine.
func (n *Node) Read(query interface{}, timeout time.Duration) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Perform a linearizable read.
	res, err := n.SyncRead(ctx, n.config.ClusterID, query)
	if err != nil {
		return nil, fmt.Errorf("failed to perform linearizable read: %w", err)
	}

	return res, nil
}

// loggingEventListener is a simple implementation of the RaftEventListener for debugging.

// type loggingEventListener struct{}

// func (l *loggingEventListener) LeaderUpdated(info events.LeaderInfo) {

// 	fmt.Printf("[Raft] Leader updated: ClusterID=%d, NodeID=%d, Term=%d\n",

// 		info.ClusterID, info.NodeID, info.Term)

// }

// func (l *loggingEventListener) NodeHostShuttingDown()                                  {}

// func (l *loggingEventListener) NodeUnreachable(info events.NodeInfo)               {}

// func (l *loggingEventListener) NodeReady(info events.NodeInfo)                     {}

// func (l *loggingEventListener) MembershipChanged(info events.MembershipInfo)       {}

// func (l *loggingEventListener) ConnectionEstablished(info events.ConnectionInfo)   {}

// func (l *loggingEventListener) ConnectionFailed(info events.ConnectionInfo)        {}

// func (l *loggingEventListener) SendSnapshotStarted(info events.SendSnapshotInfo)   {}

// func (l *loggingEventListener) SendSnapshotCompleted(info events.SendSnapshotInfo) {}

// func (l *loggingEventListener) SendSnapshotAborted(info events.SendSnapshotInfo)   {}

// func (l *loggingEventListener) SnapshotReceived(info events.SnapshotInfo)          {}

// func (l *loggingEventListener) SnapshotRecovered(info events.SnapshotInfo)         {}

// func (l *loggingEventListener) SnapshotSaved(info events.SnapshotInfo)             {}

// func (l *loggingEventListener) SnapshotCompacted(info events.SnapshotInfo)         {}

// func (l *loggingEventListener) LogCompacted(info events.LogInfo)                   {}

// func (l *loggingEventListener) LogDBCompacted(info events.LogInfo)                 {}
