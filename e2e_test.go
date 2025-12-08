package main_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Client
	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

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
	pdDataDir, _ := os.MkdirTemp("", "pd_e2e_test")
	defer os.RemoveAll(pdDataDir)
	workerDataDir, _ := os.MkdirTemp("", "worker_e2e_test")
	defer os.RemoveAll(workerDataDir)

	// Network addresses
	pdGrpcAddr := "localhost:6007"
	pdRaftAddr := "localhost:7007"
	workerGrpcAddr := "localhost:9097"
	workerRaftAddr := "localhost:9197"
	apiGrpcAddr := "localhost:8087"
	apiHttpAddr := "localhost:8187"

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

	// API Gateway
	gatewayCmd := exec.Command(
		"./bin/apigateway",
	)
	gatewayCmd.Env = os.Environ()
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("GRPC_ADDR=%s", apiGrpcAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("HTTP_ADDR=%s", apiHttpAddr))
	gatewayCmd.Env = append(gatewayCmd.Env, fmt.Sprintf("PLACEMENT_DRIVER=%s", pdGrpcAddr))
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

	time.Sleep(3 * time.Second)

	// --- Test Logic using Go Client ---
	client, err := vectron.NewClient(apiGrpcAddr, "")
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
	upserted, err := client.Upsert(collectionName, points)
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
