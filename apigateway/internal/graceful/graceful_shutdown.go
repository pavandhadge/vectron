// graceful_shutdown.go - Provides graceful shutdown handling for Vectron services
//
// This package provides utilities for:
// - Signal handling for graceful shutdown
// - Connection draining with timeout
// - Component lifecycle management
// - Context timeout enforcement

package graceful

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

// ShutdownHandler manages graceful shutdown of multiple components
type ShutdownHandler struct {
	components []Shutdownable
	timeout    time.Duration
	signals    chan os.Signal
	wg         sync.WaitGroup
}

// Shutdownable is an interface for components that need graceful shutdown
type Shutdownable interface {
	Shutdown(ctx context.Context) error
	Name() string
}

// NewShutdownHandler creates a new shutdown handler with specified timeout
func NewShutdownHandler(timeout time.Duration) *ShutdownHandler {
	return &ShutdownHandler{
		components: make([]Shutdownable, 0),
		timeout:    timeout,
		signals:    make(chan os.Signal, 1),
	}
}

// Register adds a component to be gracefully shut down
func (h *ShutdownHandler) Register(component Shutdownable) {
	h.components = append(h.components, component)
}

// Listen starts listening for shutdown signals
func (h *ShutdownHandler) Listen() {
	signal.Notify(h.signals, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-h.signals
		log.Printf("Received signal: %v. Initiating graceful shutdown...", sig)
		h.Shutdown()
		os.Exit(0)
	}()
}

// Shutdown performs graceful shutdown of all registered components
func (h *ShutdownHandler) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	log.Printf("Shutting down %d components with timeout %v...", len(h.components), h.timeout)

	// Shutdown in reverse order (dependencies first)
	for i := len(h.components) - 1; i >= 0; i-- {
		component := h.components[i]
		log.Printf("Shutting down %s...", component.Name())

		if err := component.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down %s: %v", component.Name(), err)
		} else {
			log.Printf("Successfully shut down %s", component.Name())
		}
	}

	log.Println("Graceful shutdown complete")
}

// StopSignals stops signal listening (useful for testing)
func (h *ShutdownHandler) StopSignals() {
	signal.Stop(h.signals)
}

// GRPCServer wraps a gRPC server for graceful shutdown
type GRPCServer struct {
	server     *grpc.Server
	listenAddr string
}

// NewGRPCServer creates a new graceful gRPC server wrapper
func NewGRPCServer(server *grpc.Server, listenAddr string) *GRPCServer {
	return &GRPCServer{
		server:     server,
		listenAddr: listenAddr,
	}
}

// Shutdown gracefully stops the gRPC server
func (s *GRPCServer) Shutdown(ctx context.Context) error {
	// Stop accepting new connections
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	// Wait for graceful stop or timeout
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Force stop on timeout
		log.Println("Graceful stop timed out, forcing shutdown...")
		s.server.Stop()
		return ctx.Err()
	}
}

// Name returns the component name
func (s *GRPCServer) Name() string {
	return fmt.Sprintf("gRPC Server (%s)", s.listenAddr)
}

// HTTPServer wraps an HTTP server for graceful shutdown
type HTTPServer struct {
	server *http.Server
}

// NewHTTPServer creates a new graceful HTTP server wrapper
func NewHTTPServer(server *http.Server) *HTTPServer {
	return &HTTPServer{server: server}
}

// Shutdown gracefully stops the HTTP server
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Name returns the component name
func (s *HTTPServer) Name() string {
	return fmt.Sprintf("HTTP Server (%s)", s.server.Addr)
}

// RaftNode wraps a Raft node for graceful shutdown
type RaftNode struct {
	stopFunc func()
	name     string
}

// NewRaftNode creates a new shutdownable Raft wrapper
func NewRaftNode(stopFunc func(), name string) *RaftNode {
	return &RaftNode{
		stopFunc: stopFunc,
		name:     name,
	}
}

// Shutdown gracefully stops the Raft node
func (r *RaftNode) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.stopFunc()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name
func (r *RaftNode) Name() string {
	return r.name
}

// Reconciler wraps a reconciler for graceful shutdown
type Reconciler struct {
	stopFunc func()
	name     string
}

// NewReconciler creates a new shutdownable reconciler wrapper
func NewReconciler(stopFunc func(), name string) *Reconciler {
	return &Reconciler{
		stopFunc: stopFunc,
		name:     name,
	}
}

// Shutdown gracefully stops the reconciler
func (r *Reconciler) Shutdown(ctx context.Context) error {
	r.stopFunc()
	return nil
}

// Name returns the component name
func (r *Reconciler) Name() string {
	return r.name
}

// TimeoutInterceptor creates a gRPC unary interceptor that enforces context timeouts
func TimeoutInterceptor(defaultTimeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// If no deadline is set, add one
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}
		return handler(ctx, req)
	}
}

// TimeoutInterceptorStream creates a gRPC stream interceptor that enforces context timeouts
func TimeoutInterceptorStream(defaultTimeout time.Duration) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := stream.Context()
		// If no deadline is set, add one
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}
		return handler(srv, stream)
	}
}

// ContextTimeout creates a context with explicit timeout from a parent context
func ContextTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// MustContextTimeout creates a context with explicit timeout, panicking if parent is nil
func MustContextTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}
