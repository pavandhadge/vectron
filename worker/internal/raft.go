package internal

import (
	"fmt"

	"github.com/lni/dragonboat/v3/raftio"
)

// loggingEventListener is a simple implementation of the RaftEventListener for debugging.
type loggingEventListener struct{}

func NewLoggingEventListener() *loggingEventListener {
	return &loggingEventListener{}
}

func (l *loggingEventListener) LeaderUpdated(info raftio.LeaderInfo) {
	fmt.Printf("[System Event] Leader updated: ClusterID=%d, NodeID=%d, Term=%d, LeaderID=%d\n",
		info.ClusterID, info.NodeID, info.Term, info.LeaderID)
}

func (l *loggingEventListener) NodeHostShuttingDown() {
	fmt.Printf("[System Event] NodeHost shutting down\n")
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
