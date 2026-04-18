package segment

import (
	"time"
)

type SegmentID string

type SegmentState int

const (
	SegmentStateMutable SegmentState = iota
	SegmentStateSealing
	SegmentStateImmutable
	SegmentStateCompacting
	SegmentStateObsolete
)

func (s SegmentState) String() string {
	switch s {
	case SegmentStateMutable:
		return "mutable"
	case SegmentStateSealing:
		return "sealing"
	case SegmentStateImmutable:
		return "immutable"
	case SegmentStateCompacting:
		return "compacting"
	case SegmentStateObsolete:
		return "obsolete"
	default:
		return "unknown"
	}
}

type SegmentMeta struct {
	ID            SegmentID
	State         SegmentState
	CreatedAt     time.Time
	SealedAt      time.Time
	VectorCount   int64
	BytesEstimate int64
	FilePath      string
	MinVectorID   string
	MaxVectorID   string
}

type ShardManifest struct {
	ShardID           uint64
	Version           uint64
	MutableSegmentID  SegmentID
	ImmutableSegments []SegmentID
	TombstoneEpoch    int64
	CompactionMeta    *CompactionMeta
	UpdatedAt         time.Time
}

type CompactionMeta struct {
	LastCompactionAt time.Time
	CompactionCount  int64
}

type Thresholds struct {
	MaxVectors      int
	MaxBytes        int64
	MaxAge          time.Duration
	CompactionFanIn int
}

var DefaultThresholds = Thresholds{
	MaxVectors:      100000,
	MaxBytes:        512 * 1024 * 1024,
	MaxAge:          10 * time.Minute,
	CompactionFanIn: 4,
}

type Config struct {
	Thresholds Thresholds
	ShardPath  string
	Dimension  int
	Namespace  []byte
	HNSWConfig HNSWConfig
}

type HNSWConfig struct {
	M                 int
	EfConstruction    int
	EfSearch          int
	Distance          string
	NormalizeVectors  bool
	QuantizeVectors   bool
	SearchParallelism int
}
