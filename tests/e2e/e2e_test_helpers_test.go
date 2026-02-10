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
	"path/filepath"
	"runtime"
	"strings"
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

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
		dir = parent
	}
}

func repoTempBase() (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, "temp_vectron")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return base, nil
}

// RepoTempDir creates a temporary directory under ./temp_vectron in the repo.
func RepoTempDir(prefix string) (string, error) {
	base, err := repoTempBase()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(base, prefix)
}

// BinPath returns the absolute path to a built binary under ./bin.
func BinPath(name string) (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", name+exeSuffix()), nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func buildE2EBinaries() error {
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	bins := []struct {
		name string
		pkg  string
	}{
		{name: "placementdriver", pkg: "./placementdriver/cmd/placementdriver"},
		{name: "worker", pkg: "./worker/cmd/worker"},
		{name: "apigateway", pkg: "./apigateway/cmd/apigateway"},
		{name: "authsvc", pkg: "./auth/service/cmd/auth"},
		{name: "reranker", pkg: "./reranker/cmd/reranker"},
	}

	for _, bin := range bins {
		outPath := filepath.Join(binDir, bin.name+exeSuffix())
		cmd := exec.Command("go", "build", "-trimpath", "-o", outPath, bin.pkg)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s: %w", bin.name, err)
		}
	}
	return nil
}

func appendDistributedCacheEnv(env []string) []string {
	addr := os.Getenv("DISTRIBUTED_CACHE_ADDR")
	if addr == "" {
		return env
	}
	env = append(env, "DISTRIBUTED_CACHE_ADDR="+addr)
	if v := os.Getenv("DISTRIBUTED_CACHE_PASSWORD"); v != "" {
		env = append(env, "DISTRIBUTED_CACHE_PASSWORD="+v)
	}
	if v := os.Getenv("DISTRIBUTED_CACHE_DB"); v != "" {
		env = append(env, "DISTRIBUTED_CACHE_DB="+v)
	}
	if v := os.Getenv("DISTRIBUTED_CACHE_TTL_MS"); v != "" {
		env = append(env, "DISTRIBUTED_CACHE_TTL_MS="+v)
	}
	if v := os.Getenv("DISTRIBUTED_CACHE_TIMEOUT_MS"); v != "" {
		env = append(env, "DISTRIBUTED_CACHE_TIMEOUT_MS="+v)
	}
	return env
}

func startValkeyForTests() func() {
	if os.Getenv("DISTRIBUTED_CACHE_ADDR") != "" {
		return func() {}
	}
	if _, err := exec.LookPath("podman"); err != nil {
		log.Printf("podman not found; distributed cache disabled for tests")
		return func() {}
	}

	const container = "vectron-valkey-test"
	const addr = "127.0.0.1:6379"

	isRunning := func() bool {
		out, err := exec.Command("podman", "ps", "--format", "{{.Names}}").Output()
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) == container {
				return true
			}
		}
		return false
	}

	if isRunning() {
		_ = os.Setenv("DISTRIBUTED_CACHE_ADDR", addr)
		return func() {}
	}

	exists := exec.Command("podman", "container", "exists", container).Run() == nil
	if exists {
		if err := exec.Command("podman", "start", container).Run(); err == nil {
			_ = os.Setenv("DISTRIBUTED_CACHE_ADDR", addr)
			return func() { _ = exec.Command("podman", "stop", container).Run() }
		}
		log.Printf("failed to start existing Valkey container; continuing without distributed cache")
		return func() {}
	}

	if err := exec.Command(
		"podman", "run", "-d",
		"--name", container,
		"-p", "6379:6379",
		"valkey/valkey:latest",
		"valkey-server",
		"--save", "",
		"--appendonly", "no",
		"--maxmemory", "1gb",
		"--maxmemory-policy", "allkeys-lru",
	).Run(); err != nil {
		log.Printf("failed to start Valkey container; continuing without distributed cache: %v", err)
		return func() {}
	}

	_ = os.Setenv("DISTRIBUTED_CACHE_ADDR", addr)
	return func() { _ = exec.Command("podman", "stop", container).Run() }
}

// StartEmbeddedEtcd starts a temporary embedded etcd server.
func StartEmbeddedEtcd(ctx context.Context) (*EtcdProcess, error) {
	base, err := repoTempBase()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repo temp dir: %w", err)
	}
	dataDir := filepath.Join(base, fmt.Sprintf("test-e2e-etcd-%s.etcd", uuid.New().String()))

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
	setProcessGroup(cmd)

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
		killProcess(sp.Cmd)
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
	Jwt    string
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
	c.Jwt = resp.JwtToken
	return nil
}

// CreateAPIKey creates a new API key using the stored JWT for authentication.
func (c *AuthClient) CreateAPIKey(ctx context.Context, name string) (*authpb.APIKey, error) {
	if c.Jwt == "" {
		return nil, fmt.Errorf("must login before creating an API key")
	}

	// Create a new context with the JWT metadata.
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.Jwt})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	resp, err := c.Client.CreateAPIKey(authCtx, &authpb.CreateAPIKeyRequest{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}
	return resp.KeyInfo, nil
}

// GetSDKJWT is a convenience helper that creates an API key and returns an SDK JWT for it.
func (c *AuthClient) GetSDKJWT(ctx context.Context, keyName string) (string, error) {
	apiKey, err := c.CreateAPIKey(ctx, keyName)
	if err != nil {
		return "", fmt.Errorf("failed to create api key for sdk jwt: %w", err)
	}

	sdkJwt, err := c.CreateSDKJWT(ctx, apiKey.KeyPrefix)
	if err != nil {
		return "", fmt.Errorf("failed to create sdk jwt: %w", err)
	}
	return sdkJwt, nil
}

// GetUserProfile retrieves the user's profile using the stored JWT for authentication.
func (c *AuthClient) GetUserProfile(ctx context.Context) (*authpb.GetUserProfileResponse, error) {
	if c.Jwt == "" {
		return nil, fmt.Errorf("must login before getting user profile")
	}

	// Create a new context with the JWT metadata.
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.Jwt})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	resp, err := c.Client.GetUserProfile(authCtx, &authpb.GetUserProfileRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	return resp, nil
}

// CreateSDKJWT creates a new SDK JWT for an existing API key.
func (c *AuthClient) CreateSDKJWT(ctx context.Context, apiKeyId string) (string, error) {
	if c.Jwt == "" {
		return "", fmt.Errorf("must login before creating an SDK JWT")
	}

	// Create a new context with the JWT metadata.
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.Jwt})
	authCtx := metadata.NewOutgoingContext(ctx, md)

	resp, err := c.Client.CreateSDKJWT(authCtx, &authpb.CreateSDKJWTRequest{
		ApiKeyId: apiKeyId,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create SDK JWT: %w", err)
	}
	return resp.SdkJwt, nil
}

// NewAuthenticatedContext creates a new context with the stored JWT for authentication.
func (c *AuthClient) NewAuthenticatedContext(ctx context.Context) context.Context {
	if c.Jwt == "" {
		panic("AuthClient not logged in, JWT is empty")
	}
	md := metadata.New(map[string]string{"authorization": "Bearer " + c.Jwt})
	return metadata.NewOutgoingContext(ctx, md)
}
