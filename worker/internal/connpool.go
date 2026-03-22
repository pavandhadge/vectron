// connpool.go - Connection pool for gRPC connections
//
// This file provides a connection pool that reuses gRPC connections
// to reduce connection setup overhead and improve throughput.

package internal

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ConnPool manages a pool of gRPC connections
type ConnPool struct {
	mu    sync.RWMutex
	conns map[string]*poolConn
	opts  []grpc.DialOption
}

type poolConn struct {
	conn      *grpc.ClientConn
	lastUsed  time.Time
	createdAt time.Time
}

// NewConnPool creates a new connection pool with the given options
func NewConnPool(opts ...grpc.DialOption) *ConnPool {
	// Default options for better performance
	defaultOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithInitialWindowSize(1 << 20),     // 1MB
		grpc.WithInitialConnWindowSize(1 << 20), // 1MB
		grpc.WithReadBufferSize(64 * 1024),      // 64KB
		grpc.WithWriteBufferSize(64 * 1024),     // 64KB
	}

	// Append user options (they can override defaults)
	allOpts := append(defaultOpts, opts...)

	return &ConnPool{
		conns: make(map[string]*poolConn),
		opts:  allOpts,
	}
}

// GetConn returns an existing connection or creates a new one
func (p *ConnPool) GetConn(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	// Try to get existing connection
	p.mu.RLock()
	if pc, ok := p.conns[addr]; ok && pc.conn != nil {
		// Check if connection is still healthy
		if pc.conn.GetState().String() != "SHUTDOWN" {
			pc.lastUsed = time.Now()
			p.mu.RUnlock()
			return pc.conn, nil
		}
	}
	p.mu.RUnlock()

	// Create new connection
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if pc, ok := p.conns[addr]; ok && pc.conn != nil {
		if pc.conn.GetState().String() != "SHUTDOWN" {
			pc.lastUsed = time.Now()
			return pc.conn, nil
		}
		// Close the old connection
		pc.conn.Close()
	}

	// Create new connection
	conn, err := grpc.DialContext(ctx, addr, p.opts...)
	if err != nil {
		return nil, err
	}

	p.conns[addr] = &poolConn{
		conn:      conn,
		lastUsed:  time.Now(),
		createdAt: time.Now(),
	}

	return conn, nil
}

// Close closes all connections in the pool
func (p *ConnPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, pc := range p.conns {
		if pc.conn != nil {
			pc.conn.Close()
		}
		delete(p.conns, addr)
	}
}

// Cleanup removes connections that haven't been used for the given duration
func (p *ConnPool) Cleanup(maxIdle time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for addr, pc := range p.conns {
		if now.Sub(pc.lastUsed) > maxIdle {
			if pc.conn != nil {
				pc.conn.Close()
			}
			delete(p.conns, addr)
		}
	}
}

// Size returns the number of connections in the pool
func (p *ConnPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}
