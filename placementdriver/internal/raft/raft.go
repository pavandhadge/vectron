package raft

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"path/filepath"
	"strings"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	"github.com/lni/dragonboat/v3/raftio"
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

	listener := &loggingEventListener{}

	// Configure the NodeHost, which manages the Raft nodes.
	nhc := config.NodeHostConfig{
		DeploymentID:        1, // A unique ID for the entire deployment.
		NodeHostDir:         nhDataDir,
		RaftAddress:         cfg.RaftAddress,
		ListenAddress:       cfg.RaftAddress, // Use the same address for simplicity.
		EnableMetrics:       true,
		RTTMillisecond:      200,
		RaftEventListener:   listener,
		SystemEventListener: listener,
	}
	applyLogDBProfile(&nhc)

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

func applyLogDBProfile(nhc *config.NodeHostConfig) {
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("VECTRON_LOGDB_PROFILE")))
	switch profile {
	case "":
		// Default to small to avoid large LogDB preallocation on light workloads.
		nhc.Expert.LogDB = config.GetSmallMemLogDBConfig()
	case "tiny":
		nhc.Expert.LogDB = config.GetTinyMemLogDBConfig()
	case "small":
		nhc.Expert.LogDB = config.GetSmallMemLogDBConfig()
	case "medium":
		nhc.Expert.LogDB = config.GetMediumMemLogDBConfig()
	case "large":
		nhc.Expert.LogDB = config.GetLargeMemLogDBConfig()
	case "default":
		// Leave empty to let Dragonboat choose its default.
	default:
		log.Printf("Unknown VECTRON_LOGDB_PROFILE=%q, using small", profile)
		nhc.Expert.LogDB = config.GetSmallMemLogDBConfig()
	}
	applyLogDBOverrides(nhc)
}

func applyLogDBOverrides(nhc *config.NodeHostConfig) {
	if nhc == nil {
		return
	}
	cfg := nhc.Expert.LogDB
	if v := envUintMB("VECTRON_LOGDB_WRITE_BUFFER_MB"); v > 0 {
		cfg.KVWriteBufferSize = v
	}
	if v := envUint("VECTRON_LOGDB_MAX_WRITE_BUFFERS"); v > 0 {
		cfg.KVMaxWriteBufferNumber = v
	}
	if v := envUint("VECTRON_LOGDB_KEEP_LOGS"); v > 0 {
		cfg.KVKeepLogFileNum = v
	}
	if v := envUint("VECTRON_LOGDB_RECYCLE_LOGS"); v > 0 {
		cfg.KVRecycleLogFileNum = v
	}
	nhc.Expert.LogDB = cfg
}

func envUint(key string) uint64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(val, 10, 64)
	if err != nil || parsed == 0 {
		return 0
	}
	return parsed
}

func envUintMB(key string) uint64 {
	mb := envUint(key)
	if mb == 0 {
		return 0
	}
	return mb * 1024 * 1024
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

// GetLeaderID returns the leader node ID of the given cluster.
func (n *Node) GetLeaderID(clusterID uint64) (uint64, bool, error) {
	leaderID, ok, err := n.NodeHost.GetLeaderID(clusterID)
	return leaderID, ok, err
}

// loggingEventListener is a simple implementation of the RaftEventListener for debugging.
type loggingEventListener struct{}

// Raft events
func (l *loggingEventListener) LeaderUpdated(info raftio.LeaderInfo) {
	fmt.Printf("[Raft Event] Leader updated: ClusterID=%d, NodeID=%d, Term=%d\n",
		info.ClusterID, info.NodeID, info.Term)
}

// System events
func (l *loggingEventListener) NodeHostShuttingDown() {
	fmt.Println("[System Event] NodeHost shutting down")
}

func (l *loggingEventListener) NodeUnloaded(info raftio.NodeInfo) {
	fmt.Printf("[System Event] Node unloaded: ClusterID=%d, NodeID=%d\n",
		info.ClusterID, info.NodeID)
}

func (l *loggingEventListener) NodeReady(info raftio.NodeInfo) {
	fmt.Printf("[System Event] Node ready: ClusterID=%d, NodeID=%d\n",
		info.ClusterID, info.NodeID)
}

func (l *loggingEventListener) MembershipChanged(info raftio.NodeInfo) {
	fmt.Printf("[System Event] Membership changed: ClusterID=%d, NodeID=%d\n",
		info.ClusterID, info.NodeID)
}

func (l *loggingEventListener) ConnectionEstablished(info raftio.ConnectionInfo) {
	fmt.Printf("[System Event] Connection established: Address=%s, SnapshotConnection=%v\n",
		info.Address, info.SnapshotConnection)
}

func (l *loggingEventListener) ConnectionFailed(info raftio.ConnectionInfo) {
	fmt.Printf("[System Event] Connection failed: Address=%s, SnapshotConnection=%v\n",
		info.Address, info.SnapshotConnection)
}

func (l *loggingEventListener) SendSnapshotStarted(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Send snapshot started: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SendSnapshotCompleted(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Send snapshot completed: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SendSnapshotAborted(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Send snapshot aborted: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SnapshotReceived(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Snapshot received: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SnapshotRecovered(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Snapshot recovered: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SnapshotCreated(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Snapshot created: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) SnapshotCompacted(info raftio.SnapshotInfo) {
	fmt.Printf("[System Event] Snapshot compacted: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) LogCompacted(info raftio.EntryInfo) {
	fmt.Printf("[System Event] Log compacted: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}

func (l *loggingEventListener) LogDBCompacted(info raftio.EntryInfo) {
	fmt.Printf("[System Event] Log DB compacted: ClusterID=%d, NodeID=%d, Index=%d\n",
		info.ClusterID, info.NodeID, info.Index)
}
