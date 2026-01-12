package testutil

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url" // Import the url package
	"os"
	"os/exec" // Still needed for auth service binary
	"time"

	"github.com/google/uuid"
	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

// EtcdProcess represents a running embedded etcd process for testing.
type EtcdProcess struct {
	EtcdServer *embed.Etcd
	DataDir    string
	ClientURL  string
	metricsURL string // Not strictly needed for auth service to connect, but good to have
}

// StartEmbeddedEtcd starts a temporary embedded etcd server.
// It returns an EtcdProcess struct and an error.
func StartEmbeddedEtcd(ctx context.Context) (*EtcdProcess, error) {
	dataDir := fmt.Sprintf("%s/test-distributed-etcd-%s.etcd", os.TempDir(), uuid.New().String())

	cfg := embed.NewConfig()
	cfg.Dir = dataDir

	clientPort := GetFreePort()
	peerPort := GetFreePort()

	clientURLStr := fmt.Sprintf("http://127.0.0.1:%d", clientPort)
	peerURLStr := fmt.Sprintf("http://127.0.0.1:%d", peerPort)

	// Parse URL strings into url.URL objects
	clientURL, err := url.Parse(clientURLStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client URL: %w", err)
	}
	peerURL, err := url.Parse(peerURLStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse peer URL: %w", err)
	}

	cfg.ListenClientUrls = []url.URL{*clientURL}
	cfg.AdvertiseClientUrls = []url.URL{*clientURL}
	cfg.ListenPeerUrls = []url.URL{*peerURL}
	cfg.AdvertisePeerUrls = []url.URL{*peerURL}
	cfg.InitialCluster = fmt.Sprintf("default=%s", peerURLStr) // InitialCluster expects a string

	// Start etcd
	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to start embedded etcd: %w", err)
	}

	select {
	case <-e.Server.ReadyNotify():
		log.Printf("Embedded etcd started, client URL: %s", clientURLStr)
	case <-ctx.Done():
		e.Server.Stop() // Trigger a shutdown
		return nil, fmt.Errorf("context cancelled while waiting for etcd to start: %w", ctx.Err())
	case <-time.After(30 * time.Second): // General timeout
		e.Server.Stop() // Trigger a shutdown
		return nil, fmt.Errorf("embedded etcd server took too long to start")
	}

	return &EtcdProcess{
		EtcdServer: e,
		DataDir:    dataDir,
		ClientURL:  clientURLStr,
		metricsURL: "", // Not exposed via embed.Config directly without more specific setup
	}, nil
}

// Stop stops the embedded etcd process and cleans up its data directory.
func (ep *EtcdProcess) Stop() {
	if ep.EtcdServer != nil {
		ep.EtcdServer.Close()
		log.Printf("Embedded etcd stopped.")
	}
	if ep.DataDir != "" {
		log.Printf("Cleaning up etcd data directory: %s", ep.DataDir)
		if err := os.RemoveAll(ep.DataDir); err != nil {
			log.Printf("Failed to remove etcd data directory %s: %v", ep.DataDir, err)
		}
	}
}

// GetFreePort finds an available network port.
func GetFreePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("failed to resolve TCP address: %v", err))
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(fmt.Sprintf("failed to listen on TCP port: %v", err))
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// AuthServerProcess represents a running auth service process.
type AuthServerProcess struct {
	Cmd        *exec.Cmd
	GRPCPort   int
	HTTPPort   int
	Client     authpb.AuthServiceClient // Client for this specific server
	GrpcClient *grpc.ClientConn
}

// StartAuthService starts the auth service binary in a separate process.
func StartAuthService(ctx context.Context, etcdClientURL, jwtSecret string) (*AuthServerProcess, error) {
	grpcPort := GetFreePort()
	httpPort := GetFreePort()

	cmdArgs := []string{
		// Path to the built auth service binary (assuming it's in the current directory after `go build`)
		"./auth_service",
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("GRPC_PORT=:%d", grpcPort))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HTTP_PORT=:%d", httpPort))
	cmd.Env = append(cmd.Env, fmt.Sprintf("ETCD_ENDPOINTS=%s", etcdClientURL))
	cmd.Env = append(cmd.Env, fmt.Sprintf("JWT_SECRET=%s", jwtSecret))

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr // Direct service output to stderr for debugging

	log.Printf("Starting auth service with GRPC_PORT=:%d, HTTP_PORT=:%d, ETCD_ENDPOINTS=%s", grpcPort, httpPort, etcdClientURL)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start auth service: %w", err)
	}

	// Wait for the gRPC server to be ready
	// This is a simple poll, a more robust solution might involve a readiness endpoint on the service
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", grpcPort)
	conn, err := WaitForGrpcService(ctx, grpcAddr)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("auth service gRPC not ready: %w", err)
	}

	return &AuthServerProcess{
		Cmd:        cmd,
		GRPCPort:   grpcPort,
		HTTPPort:   httpPort,
		GrpcClient: conn,
		Client:     authpb.NewAuthServiceClient(conn),
	}, nil
}

// Stop stops the auth service process.
func (asp *AuthServerProcess) Stop() {
	if asp.GrpcClient != nil {
		asp.GrpcClient.Close()
	}
	if asp.Cmd != nil && asp.Cmd.Process != nil {
		log.Printf("Stopping auth service process (PID: %d)", asp.Cmd.Process.Pid)
		if err := asp.Cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill auth service process (PID: %d): %v", asp.Cmd.Process.Pid, err)
		}
		_ = asp.Cmd.Wait()
	}
}

// WaitForGrpcService attempts to connect to a gRPC service until successful or timeout.
func WaitForGrpcService(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return nil, fmt.Errorf("timeout waiting for gRPC service at %s: %w", addr, timeoutCtx.Err())
		default:
			conn, err := grpc.DialContext(timeoutCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err == nil {
				return conn, nil
			}
			log.Printf("Waiting for gRPC service at %s: %v", addr, err)
			time.Sleep(500 * time.Millisecond)
		}
	}
}
