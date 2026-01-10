package main_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing" // Added for *testing.T in ServiceProcess.Stop
	"time"

	"github.com/google/uuid"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// EtcdProcess represents a running embedded etcd process for testing.
type EtcdProcess struct {
	EtcdServer *embed.Etcd
	DataDir    string
	ClientURL  string
}

// StartEmbeddedEtcd starts a temporary embedded etcd server.
func StartEmbeddedEtcd(ctx context.Context) (*EtcdProcess, error) {
	dataDir := fmt.Sprintf("%s/test-e2e-etcd-%s.etcd", os.TempDir(), uuid.New().String())

	cfg := embed.NewConfig()
	cfg.Dir = dataDir

	clientPort := GetFreePort()
	peerPort := GetFreePort()

	clientURLStr := fmt.Sprintf("http://127.0.0.1:%d", clientPort)
	peerURLStr := fmt.Sprintf("http://127.0.0.1:%d", peerPort)

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
	cfg.InitialCluster = fmt.Sprintf("default=%s", peerURLStr)

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to start embedded etcd: %w", err)
	}

	select {
	case <-e.Server.ReadyNotify():
		log.Printf("Embedded etcd started, client URL: %s", clientURLStr)
	case <-ctx.Done():
		e.Server.Stop()
		return nil, fmt.Errorf("context cancelled while waiting for etcd to start: %w", ctx.Err())
	case <-time.After(60 * time.Second):
		e.Server.Stop()
		return nil, fmt.Errorf("embedded etcd server took too long to start")
	}

	return &EtcdProcess{
		EtcdServer: e,
		DataDir:    dataDir,
		ClientURL:  clientURLStr,
	}, nil
}

// Stop stops the embedded etcd process and cleans up its data directory.
func (ep *EtcdProcess) Stop() {
	if ep.EtcdServer != nil {
		ep.EtcdServer.Close()
	}
	if ep.DataDir != "" {
		os.RemoveAll(ep.DataDir)
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

// ServiceProcess represents a running service process.
type ServiceProcess struct {
	Cmd    *exec.Cmd
	out    bytes.Buffer
	Cancel context.CancelFunc
}

// StartService starts a service binary in a separate process.
func StartService(ctx context.Context, name string, env []string, args ...string) (*ServiceProcess, error) {
	ctx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = os.Environ()
	cleanEnv := make([]string, 0, len(cmd.Env))
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "GRPC_ADDR=") &&
			!strings.HasPrefix(e, "HTTP_ADDR=") &&
			!strings.HasPrefix(e, "ETCD_ENDPOINTS=") &&
			!strings.HasPrefix(e, "JWT_SECRET=") &&
			!strings.HasPrefix(e, "PLACEMENT_DRIVER=") &&
			!strings.HasPrefix(e, "AUTH_SERVICE_ADDR=") &&
			!strings.HasPrefix(e, "PD_ADDRS=") &&
			!strings.HasPrefix(e, "NODE_ID=") {
			cleanEnv = append(cleanEnv, e)
		}
	}
	cmd.Env = append(cleanEnv, env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	process := &ServiceProcess{
		Cmd:    cmd,
		Cancel: cancel,
	}
	cmd.Stdout = &process.out
	cmd.Stderr = &process.out

	log.Printf("Starting %s with command: %s %v", name, args[0], args[1:])
	log.Printf("  Environment variables for %s: %v", name, cmd.Env)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start %s: %w", name, err)
	}
	log.Printf("%s started with PID %d", name, cmd.Process.Pid)

	return process, nil
}

// Stop stops the service process.
func (sp *ServiceProcess) Stop(t *testing.T) {
	if sp.Cmd != nil && sp.Cmd.Process != nil {
		syscall.Kill(-sp.Cmd.Process.Pid, syscall.SIGKILL)
	}
	sp.Cancel()
	if t.Failed() {
		t.Logf("%s Output:\n%s", sp.Cmd.Path, sp.out.String())
	}
}

// WaitForGrpcService attempts to connect to a gRPC service until successful or timeout.
func WaitForGrpcService(ctx context.Context, addr string, timeout time.Duration) (*grpc.ClientConn, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return nil, fmt.Errorf("timeout waiting for gRPC service at %s: %w", addr, timeoutCtx.Err())
		default:
			conn, err := grpc.DialContext(timeoutCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err == nil {
				log.Printf("Successfully connected to gRPC service at %s", addr)
				return conn, nil
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// AuthClient is a helper client for the auth service.
type AuthClient struct {
	Conn   *grpc.ClientConn
	Client authpb.AuthServiceClient
	jwt    string
}

// NewAuthClient creates a new client for the auth service.
func NewAuthClient(ctx context.Context, addr string) (*AuthClient, error) {
	conn, err := WaitForGrpcService(ctx, addr, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return &AuthClient{
		Conn:   conn,
		Client: authpb.NewAuthServiceClient(conn),
	}, nil
}

// Close closes the connection to the auth service.
func (c *AuthClient) Close() {
	if c.Conn != nil {
		c.Conn.Close()
	}
}

// Login authenticates a user and stores the JWT for subsequent authenticated calls.
func (c *AuthClient) Login(ctx context.Context, email, password string) error {
	resp, err := c.Client.Login(ctx, &authpb.LoginRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}
	c.jwt = resp.JwtToken
	return nil
}

// CreateAPIKey creates a new API key using the stored JWT for authentication.
func (c *AuthClient) CreateAPIKey(ctx context.Context, name string) (string, error) {
	if c.jwt == "" {
		return "", fmt.Errorf("must login before creating an API key")
	}

	// Create a new context with the JWT metadata.
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.jwt})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	resp, err := c.Client.CreateAPIKey(authCtx, &authpb.CreateAPIKeyRequest{
		Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create API key: %w", err)
	}
	return resp.FullKey, nil
}

