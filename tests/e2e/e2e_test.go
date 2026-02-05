// Run: make test-e2e

package main_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Client
	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

const jwtTestSecret = "CHANGE_ME_IN_PRODUCTION_FOR_TESTS_ONLY_32CHARS_MIN"

func generateTestJWT(userID, plan, apiKeyID string) (string, error) {
	claims := struct {
		UserID   string `json:"user_id"`
		Plan     string `json:"plan"`
		APIKeyID string `json:"api_key_id"`
		jwt.RegisteredClaims
	}{
		UserID:   userID,
		Plan:     plan,
		APIKeyID: apiKeyID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "vectron-test",
			Subject:   userID,
			ID:        apiKeyID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtTestSecret))
}

func waitFor(t *testing.T, out *bytes.Buffer, text string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for text '%s'", text)
		}
		if bytes.Contains(out.Bytes(), []byte(text)) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func TestMain(m *testing.M) {
	// Build the binaries before running the tests
	// Get the project root directory (where go.mod is located)
	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Printf("Failed to locate repo root: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command("make", "build")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Failed to build binaries: %v\n", err)
		fmt.Printf("Working directory: %s\n", repoRoot)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestE2E_FullLifecycle(t *testing.T) {
	// --- Test Setup ---

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create temporary directories
	pdDataDir, err := RepoTempDir("pd_e2e_test-")
	require.NoError(t, err)
	defer os.RemoveAll(pdDataDir)
	workerDataDir, err := RepoTempDir("worker_e2e_test-")
	require.NoError(t, err)
	defer os.RemoveAll(workerDataDir)

	// Start Embedded Etcd (required for Auth service)
	etcd, err := StartEmbeddedEtcd(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { etcd.Stop() })

	// Auth Service
	authSvcPort, err := getFreePort()
	require.NoError(t, err)
	authSvcAddr := fmt.Sprintf("127.0.0.1:%d", authSvcPort)
	authBin, err := BinPath("authsvc")
	require.NoError(t, err)
	authCmd := exec.Command(authBin)
	authCmd.Env = os.Environ()
	authCmd.Env = append(authCmd.Env,
		fmt.Sprintf("GRPC_PORT=:%d", authSvcPort),
		fmt.Sprintf("ETCD_ENDPOINTS=%s", etcd.ClientURL),
		fmt.Sprintf("JWT_SECRET=%s", jwtTestSecret),
	)
	authCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var authOut bytes.Buffer
	authCmd.Stdout = &authOut
	authCmd.Stderr = &authOut
	require.NoError(t, authCmd.Start())
	t.Cleanup(func() {
		syscall.Kill(-authCmd.Process.Pid, syscall.SIGKILL)
		if t.Failed() {
			t.Logf("Auth Service Output:\n%s", authOut.String())
		}
	})

	// Reranker Service (required by API Gateway)
	rerankerPort, err := getFreePort()
	require.NoError(t, err)
	rerankerAddr := fmt.Sprintf("127.0.0.1:%d", rerankerPort)
	rerankerBin, err := BinPath("reranker")
	require.NoError(t, err)
	rerankerCmd := exec.Command(
		rerankerBin,
		fmt.Sprintf("--port=%d", rerankerPort),
		"--strategy=rule",
		"--cache=memory",
	)
	rerankerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var rerankerOut bytes.Buffer
	rerankerCmd.Stdout = &rerankerOut
	rerankerCmd.Stderr = &rerankerOut
	require.NoError(t, rerankerCmd.Start())
	t.Cleanup(func() {
		syscall.Kill(-rerankerCmd.Process.Pid, syscall.SIGKILL)
		if t.Failed() {
			t.Logf("Reranker Output:\n%s", rerankerOut.String())
		}
	})

	// Feedback DB path for API Gateway
	gatewayDataDir, err := RepoTempDir("gateway_e2e_test-")
	require.NoError(t, err)
	defer os.RemoveAll(gatewayDataDir)

	// Network addresses
	workerGrpcPort, err := getFreePort()
	require.NoError(t, err)
	workerGrpcAddr := fmt.Sprintf("127.0.0.1:%d", workerGrpcPort)

	workerRaftPort, err := getFreePort()
	require.NoError(t, err)
	workerRaftAddr := fmt.Sprintf("127.0.0.1:%d", workerRaftPort)

	apiGrpcPort, err := getFreePort()
	require.NoError(t, err)
	apiGrpcAddr := fmt.Sprintf("127.0.0.1:%d", apiGrpcPort)

	apiHttpPort, err := getFreePort()
	require.NoError(t, err)
	apiHttpAddr := fmt.Sprintf("127.0.0.1:%d", apiHttpPort)

	// --- Start Services as Subprocesses ---

	// Setup for a 3-node PD cluster
	pdGrpcAddrs := make([]string, 3)
	pdRaftAddrs := make([]string, 3)
	pdInitialMembers := ""
	for i := 0; i < 3; i++ {
		pdGrpcPort, err := getFreePort()
		require.NoError(t, err)
		pdGrpcAddrs[i] = fmt.Sprintf("127.0.0.1:%d", pdGrpcPort)

		pdRaftPort, err := getFreePort()
		require.NoError(t, err)
		pdRaftAddrs[i] = fmt.Sprintf("127.0.0.1:%d", pdRaftPort)

		if i > 0 {
			pdInitialMembers += ","
		}
		pdInitialMembers += fmt.Sprintf("%d:%s", i+1, pdRaftAddrs[i])
	}
	allPdGrpcAddrs := fmt.Sprintf("%s,%s,%s", pdGrpcAddrs[0], pdGrpcAddrs[1], pdGrpcAddrs[2])

	// Start Placement Drivers
	for i := 0; i < 3; i++ {
		nodeID := i + 1
		pdDataDir, err := RepoTempDir(fmt.Sprintf("pd_e2e_test_%d-", nodeID))
		require.NoError(t, err)
		defer os.RemoveAll(pdDataDir)

		pdBin, err := BinPath("placementdriver")
		require.NoError(t, err)
		pdCmd := exec.Command(
			pdBin,
			fmt.Sprintf("--node-id=%d", nodeID),
			"--cluster-id=1",
			"--raft-addr="+pdRaftAddrs[i],
			"--grpc-addr="+pdGrpcAddrs[i],
			"--data-dir="+pdDataDir,
			"--initial-members="+pdInitialMembers,
		)
		pdCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var pdOut bytes.Buffer
		pdCmd.Stdout = &pdOut
		pdCmd.Stderr = &pdOut
		require.NoError(t, pdCmd.Start())
		t.Cleanup(func() {
			syscall.Kill(-pdCmd.Process.Pid, syscall.SIGKILL)
			if t.Failed() {
				t.Logf("Placement Driver %d Output:\n%s", nodeID, pdOut.String())
			}
		})
	}
	// Give the cluster time to elect a leader.
	// A more robust way would be to query each node until a leader is reported.
	time.Sleep(5 * time.Second)

	// Worker
	workerBin, err := BinPath("worker")
	require.NoError(t, err)
	workerCmd := exec.Command(
		workerBin,
		"--node-id=1",
		"--raft-addr="+workerRaftAddr,
		"--grpc-addr="+workerGrpcAddr,
		"--pd-addrs="+allPdGrpcAddrs,
		"--data-dir="+workerDataDir,
	)
	workerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var workerOut bytes.Buffer
	workerCmd.Stdout = &workerOut
	workerCmd.Stderr = &workerOut
	require.NoError(t, workerCmd.Start())
	t.Cleanup(func() {
		syscall.Kill(-workerCmd.Process.Pid, syscall.SIGKILL)
		if t.Failed() {
			t.Logf("Worker Output:\n%s", workerOut.String())
		}
	})
	// Wait for the worker to register with the PD.
	waitFor(t, &workerOut, "Successfully registered with PD", 10*time.Second)

	// API Gateway
	gatewayBin, err := BinPath("apigateway")
	require.NoError(t, err)
	gatewayCmd := exec.Command(gatewayBin)
	gatewayCmd.Env = os.Environ()
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("GRPC_ADDR=%s", apiGrpcAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("HTTP_ADDR=%s", apiHttpAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("PLACEMENT_DRIVER=%s", allPdGrpcAddrs))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("JWT_SECRET=%s", jwtTestSecret))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("AUTH_SERVICE_ADDR=%s", authSvcAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("RERANKER_SERVICE_ADDR=%s", rerankerAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("FEEDBACK_DB_PATH=%s/feedback.db", gatewayDataDir))
	gatewayCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var gatewayOut bytes.Buffer
	gatewayCmd.Stdout = &gatewayOut
	gatewayCmd.Stderr = &gatewayOut
	require.NoError(t, gatewayCmd.Start())
	t.Cleanup(func() {
		syscall.Kill(-gatewayCmd.Process.Pid, syscall.SIGKILL)
		if t.Failed() {
			t.Logf("API Gateway Output:\n%s", gatewayOut.String())
		}
	})
	// Wait for services to be ready.
	_, err = WaitForGrpcService(ctx, authSvcAddr, 30*time.Second)
	require.NoError(t, err)
	_, err = WaitForGrpcService(ctx, rerankerAddr, 30*time.Second)
	require.NoError(t, err)
	_, err = WaitForGrpcService(ctx, apiGrpcAddr, 30*time.Second)
	require.NoError(t, err)

	// --- Test Logic using Go Client ---
	// Generate a test token
	testToken, err := generateTestJWT("test-user-123", "pro", "test-key-abc")
	require.NoError(t, err)

	client, err := vectron.NewClient(apiGrpcAddr, testToken)
	require.NoError(t, err)
	defer client.Close()

	collectionName := "e2e-test-collection"

	// 1. Create Collection
	err = client.CreateCollection(collectionName, 4, "euclidean")
	require.NoError(t, err)

	// Wait for the collection to be ready.
	require.Eventually(t, func() bool {
		status, err := client.GetCollectionStatus(collectionName)
		if err != nil {
			return false
		}
		if len(status.Shards) == 0 {
			return false
		}
		for _, shard := range status.Shards {
			if !shard.Ready {
				return false
			}
		}
		return true
	}, 20*time.Second, 1*time.Second, "timed out waiting for collection to be ready")

	// 2. List Collections
	collections, err := client.ListCollections()
	require.NoError(t, err)
	assert.Contains(t, collections, collectionName)

	// 3. Upsert Points
	points := []*vectron.Point{
		{ID: "p1", Vector: []float32{0.1, 0.2, 0.3, 0.4}},
		{ID: "p2", Vector: []float32{0.5, 0.6, 0.7, 0.8}},
	}
	var upserted int32
	// var err error
	for i := 0; i < 10; i++ {
		upserted, err = client.Upsert(collectionName, points)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NoError(t, err)
	assert.Equal(t, int32(2), upserted)

	// 4. Search
	results, err := client.Search(collectionName, []float32{0.1, 0.2, 0.3, 0.4}, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "p1", results[0].ID)

	// 5. Get Point
	point, err := client.Get(collectionName, "p2")
	require.NoError(t, err)
	assert.Equal(t, "p2", point.ID)
	assert.Equal(t, []float32{0.5, 0.6, 0.7, 0.8}, point.Vector)

	// 6. Delete Point
	err = client.Delete(collectionName, "p1")
	require.NoError(t, err)

	// 7. Get Deleted Point (should fail)
	_, err = client.Get(collectionName, "p1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), vectron.ErrNotFound.Error())
}
