package main_test

import (
	"bytes"
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

const jwtTestSecret = "CHANGE_ME_IN_PRODUCTION"

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
	cmd := exec.Command("make", "build")
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Failed to build binaries: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestE2E_FullLifecycle(t *testing.T) {
	// --- Test Setup ---

	// Create temporary directories
	pdDataDir, err := os.MkdirTemp("", "pd_e2e_test-")
	require.NoError(t, err)
	defer os.RemoveAll(pdDataDir)
	workerDataDir, err := os.MkdirTemp("", "worker_e2e_test-")
	require.NoError(t, err)
	defer os.RemoveAll(workerDataDir)

	// Network addresses
	pdGrpcPort, err := getFreePort()
	require.NoError(t, err)
	pdGrpcAddr := fmt.Sprintf("127.0.0.1:%d", pdGrpcPort)

	pdRaftPort, err := getFreePort()
	require.NoError(t, err)
	pdRaftAddr := fmt.Sprintf("127.0.0.1:%d", pdRaftPort)

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

	// Placement Driver
	pdCmd := exec.Command(
		"./bin/placementdriver",
		"--node-id=1",
		"--cluster-id=1",
		"--raft-addr="+pdRaftAddr,
		"--grpc-addr="+pdGrpcAddr,
		"--data-dir="+pdDataDir,
		"--initial-members=1:"+pdRaftAddr,
	)
	pdCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var pdOut bytes.Buffer
	pdCmd.Stdout = &pdOut
	pdCmd.Stderr = &pdOut
	require.NoError(t, pdCmd.Start())
	t.Cleanup(func() {
		syscall.Kill(-pdCmd.Process.Pid, syscall.SIGKILL)
		if t.Failed() {
			t.Logf("Placement Driver Output:\n%s", pdOut.String())
		}
	})
	// Wait for PD to be ready before starting other components.
	waitFor(t, &pdOut, "became leader", 20*time.Second)

	// Worker
	workerCmd := exec.Command(
		"./bin/worker",
		"--node-id=1",
		"--raft-addr="+workerRaftAddr,
		"--grpc-addr="+workerGrpcAddr,
		"--pd-addr="+pdGrpcAddr,
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
	gatewayCmd := exec.Command(
		"./bin/apigateway",
	)
	gatewayCmd.Env = os.Environ()
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("GRPC_ADDR=%s", apiGrpcAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("HTTP_ADDR=%s", apiHttpAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("PLACEMENT_DRIVER=%s", pdGrpcAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("JWT_SECRET=%s", jwtTestSecret))
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
	// Wait for the gateway to be ready.
	waitFor(t, &gatewayOut, "Vectron gRPC API", 10*time.Second)

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
