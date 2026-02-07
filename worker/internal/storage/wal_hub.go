package storage

import (
	"sync"
	"sync/atomic"
)

// WALUpdate represents a single HNSW WAL update for streaming.
type WALUpdate struct {
	ID     string
	Value  []byte
	Delete bool
	TS     int64
}

type walSubscriber struct {
	ch chan WALUpdate
}

// WALHub fan-outs WAL updates to streaming subscribers without backpressure.
// Slow subscribers will drop updates when their buffers are full.
type WALHub struct {
	mu      sync.RWMutex
	subs    map[uint64]map[uint64]*walSubscriber
	nextSub uint64
}

// NewWALHub creates a new WAL hub.
func NewWALHub() *WALHub {
	return &WALHub{
		subs: make(map[uint64]map[uint64]*walSubscriber),
	}
}

// Subscribe registers a subscriber for the given shard ID.
func (h *WALHub) Subscribe(shardID uint64, buffer int) (uint64, <-chan WALUpdate, func()) {
	if h == nil {
		return 0, nil, func() {}
	}
	if buffer <= 0 {
		buffer = 1024
	}
	sub := &walSubscriber{ch: make(chan WALUpdate, buffer)}
	id := atomic.AddUint64(&h.nextSub, 1)

	h.mu.Lock()
	m := h.subs[shardID]
	if m == nil {
		m = make(map[uint64]*walSubscriber)
		h.subs[shardID] = m
	}
	m[id] = sub
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if m := h.subs[shardID]; m != nil {
			if s, ok := m[id]; ok {
				delete(m, id)
				close(s.ch)
			}
			if len(m) == 0 {
				delete(h.subs, shardID)
			}
		}
		h.mu.Unlock()
	}

	return id, sub.ch, cancel
}

// Publish sends updates to all subscribers of the shard.
// Updates are dropped for slow subscribers when their buffers are full.
func (h *WALHub) Publish(shardID uint64, updates []WALUpdate) {
	if h == nil || len(updates) == 0 {
		return
	}
	h.mu.RLock()
	m := h.subs[shardID]
	if len(m) == 0 {
		h.mu.RUnlock()
		return
	}
	subs := make([]*walSubscriber, 0, len(m))
	for _, sub := range m {
		subs = append(subs, sub)
	}
	h.mu.RUnlock()

	for _, upd := range updates {
		for _, sub := range subs {
			select {
			case sub.ch <- upd:
			default:
				// Drop if subscriber is slow.
			}
		}
	}
}
