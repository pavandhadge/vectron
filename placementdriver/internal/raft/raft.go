package raft

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	"github.com/lni/dragonboat/v3/statemachine"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
)

// Config holds the configuration for the Dragonboat Raft node.
type Config struct {
	// NodeID is the unique ID of this node in the Raft cluster (must be > 0).
	NodeID uint64
	// ClusterID is the unique ID of the Raft cluster.
	ClusterID uint64
	// RaftAddress is the address used for Raft communication (e.g., "localhost:7001").
	RaftAddress string
	// InitialMembers is a map of node IDs to their Raft addresses, used for bootstrapping.
	InitialMembers map[uint64]string
	// DataDir is the root directory for storing Raft logs and snapshots.
	DataDir string
}

// Node is a wrapper around the Dragonboat NodeHost, encapsulating the Raft logic.
type Node struct {
	*dragonboat.NodeHost
	config Config
	fsm    *fsm.FSM // A reference to the underlying state machine.
}

// NewNode creates, configures, and starts a new Dragonboat Raft node.
func NewNode(cfg Config) (*Node, error) {
	var fsmInstance *fsm.FSM

	// Create a dedicated directory for the NodeHost's internal data.
	nhDataDir := filepath.Join(cfg.DataDir, fmt.Sprintf("nodehost-%d", cfg.NodeID))

	// Configure the NodeHost, which manages the Raft nodes.
	nhc := config.NodeHostConfig{
		DeploymentID:   1, // A unique ID for the entire deployment.
		NodeHostDir:    nhDataDir,
		RaftAddress:    cfg.RaftAddress,
		ListenAddress:  cfg.RaftAddress, // Use the same address for simplicity.
		EnableMetrics:  true,
		RTTMillisecond: 200,
		// RaftEventListener: &loggingEventListener{}, // Add a simple logger.
	}

	// Create the NodeHost instance.
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		return nil, fmt.Errorf("failed to create nodehost: %w", err)
	}

	// Configure the specific Raft cluster.
	rc := config.Config{
		NodeID:             cfg.NodeID,
		ClusterID:          cfg.ClusterID,
		ElectionRTT:        10, // Number of message round-trip-times for an election.
		HeartbeatRTT:       1,  // Number of message round-trip-times for a heartbeat.
		CheckQuorum:        true,
		SnapshotEntries:    1000,
		CompactionOverhead: 500,
	}

	// The factory function that Dragonboat will call to create the state machine.
	createFSM := func(clusterID uint64, nodeID uint64) statemachine.IOnDiskStateMachine {
		fsmInstance = fsm.NewFSM()
		return fsmInstance
	}

	// Start the Raft cluster on this NodeHost.
	if err := nh.StartOnDiskCluster(cfg.InitialMembers, false, createFSM, rc); err != nil {
		nh.Stop()
		return nil, fmt.Errorf("failed to start cluster: %w", err)
	}

	return &Node{
			NodeHost: nh,
			config:   cfg,
			fsm:      fsmInstance,
		},
		nil
}

// GetFSM returns a pointer to the FSM instance managed by this Raft node.
// This allows the gRPC server to directly query the state machine for reads.
func (n *Node) GetFSM() *fsm.FSM {
	return n.fsm
}

// Propose submits a command (a byte slice) to the Raft log and waits for it to be committed.
// This is used for all write operations to ensure they are replicated consistently.
func (n *Node) Propose(cmd []byte, timeout time.Duration) (statemachine.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get a session for proposing commands.
	cs := n.GetNoOPSession(n.config.ClusterID)

	// SyncPropose blocks until the command is committed or the context times out.
	res, err := n.NodeHost.SyncPropose(ctx, cs, cmd)
	if err != nil {
		return statemachine.Result{}, fmt.Errorf("failed to propose command: %w", err)
	}
	return res, nil
}

// Read performs a linearizable read from the state machine.
// This ensures that the read reflects the latest committed state of the cluster.
func (n *Node) Read(query interface{}, timeout time.Duration) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// SyncRead ensures linearizability by checking the leader's commit index.
	res, err := n.SyncRead(ctx, n.config.ClusterID, query)
	if err != nil {
		return nil, fmt.Errorf("failed to perform linearizable read: %w", err)
	}
	return res, nil
}

/*
// loggingEventListener is a simple implementation of the RaftEventListener for debugging.
type loggingEventListener struct{}

func (l *loggingEventListener) LeaderUpdated(info raftio.LeaderInfo) {
	fmt.Printf("[Raft Event] Leader updated: ClusterID=%d, NodeID=%d, Term=%d\n",
		info.ClusterID, info.NodeID, info.Term)
}

func (l *loggingEventListener) NodeHostShuttingDown() {}

func (l *loggingEventListener) NodeUnreachable(info raftio.NodeInfo) {}

func (l *loggingEventListener) NodeReady(info raftio.NodeInfo) {}

func (l *loggingEventListener) MembershipChanged(info raftio.MembershipInfo) {}

func (l *loggingEventListener) ConnectionEstablished(info raftio.ConnectionInfo) {}

func (l *loggingEventListener) ConnectionFailed(info raftio.ConnectionInfo) {}

func (l *loggingEventListener) SendSnapshotStarted(info raftio.SendSnapshotInfo) {}

func (l *loggingEventListener) SendSnapshotCompleted(info raftio.SendSnapshotInfo) {}

func (l *loggingEventListener) SendSnapshotAborted(info raftio.SendSnapshotInfo) {}

func (l *loggingEventListener) SnapshotReceived(info raftio.SnapshotInfo) {}

func (l *loggingEventListener) SnapshotRecovered(info raftio.SnapshotInfo) {}

func (l *loggingEventListener) SnapshotSaved(info raftio.SnapshotInfo) {}

func (l *loggingEventListener) SnapshotCompacted(info raftio.SnapshotInfo) {}

func (l *loggingEventListener) LogCompacted(info raftio.LogInfo) {}

func (l *loggingEventListener) LogDBCompacted(info raftio.LogInfo) {}
*/
