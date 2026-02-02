package main_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

const e2eJwtSecret = "e2e-super-secret-jwt-key-for-testing-vectron-only"

func TestE2E_FullSystem_WithAuth(t *testing.T) {
	// This test orchestrates a full end-to-end scenario, including:
	// 1. Starting a real etcd instance.
	// 2. Starting all microservices (placementdriver, worker, auth, apigateway).
	// 3. Using a dedicated client to interact with the auth service to:
	//    a. Register a new user.
	//    b. Obtain a Login JWT for that user.
	//    c. Create an API Key using the Login JWT.
	//    d. Obtain an SDK JWT for that API Key.
	// 4. Using the Go client library with the SDK JWT to interact with the apigateway.
	// 5. Performing a full lifecycle of operations:
	//    - CreateCollection
	//    - ListCollections
	//    - Upsert
	//    - Search
	//    - Get
	//    - Delete
	// 6. Tearing down all services and cleaning up data.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- 1. Build Binaries ---
	// Assumes TestMain has already built the binaries.

	// --- 2. Start Services ---

	// Start Embedded Etcd
	etcd, err := StartEmbeddedEtcd(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { etcd.Stop() })

	// Start Auth Service
	authSvcPort := GetFreePort()
	authSvcAddr := fmt.Sprintf("127.0.0.1:%d", authSvcPort)
	authSvc, err := StartService(ctx, "authsvc",
		[]string{
			fmt.Sprintf("GRPC_PORT=:%d", authSvcPort),
			fmt.Sprintf("ETCD_ENDPOINTS=%s", etcd.ClientURL),
			fmt.Sprintf("JWT_SECRET=%s", e2eJwtSecret),
		},
		"./bin/authsvc",
	)
	require.NoError(t, err)
	t.Cleanup(func() { authSvc.Stop(t) })

	// Setup for a 3-node PD cluster (copied from original e2e_test.go)
	pdGrpcAddrs := make([]string, 3)
	pdRaftAddrs := make([]string, 3)
	pdInitialMembers := ""
	for i := 0; i < 3; i++ {
		pdGrpcPort := GetFreePort()
		pdGrpcAddrs[i] = fmt.Sprintf("127.0.0.1:%d", pdGrpcPort)

		pdRaftPort := GetFreePort()
		pdRaftAddrs[i] = fmt.Sprintf("127.0.0.1:%d", pdRaftPort)

		if i > 0 {
			pdInitialMembers += ","
		}
		pdInitialMembers += fmt.Sprintf("%d:%s", i+1, pdRaftAddrs[i])
	}
	allPdGrpcAddrs := strings.Join(pdGrpcAddrs, ",")

	// Start Placement Drivers (copied from original e2e_test.go)
	for i := 0; i < 3; i++ {
		nodeID := i + 1
		pdDataDirNode, err := os.MkdirTemp("", fmt.Sprintf("pd_e2e_full_test_node%d-", nodeID))
		require.NoError(t, err)
		t.Cleanup(func(nodeID int, dir string) func() {
			return func() { os.RemoveAll(dir) }
		}(nodeID, pdDataDirNode)) // Capture nodeID and dir for cleanup

		pd, err := StartService(ctx, fmt.Sprintf("placementdriver-node%d", nodeID),
			[]string{}, // Environment variables (none for PD)
			"./bin/placementdriver",
			fmt.Sprintf("--node-id=%d", nodeID),
			"--cluster-id=1",
			fmt.Sprintf("--raft-addr=%s", pdRaftAddrs[i]),
			fmt.Sprintf("--grpc-addr=%s", pdGrpcAddrs[i]),
			fmt.Sprintf("--data-dir=%s", pdDataDirNode),
			fmt.Sprintf("--initial-members=%s", pdInitialMembers),
		)
		require.NoError(t, err)
		t.Cleanup(func(pdService *ServiceProcess) func() {
			return func() { pdService.Stop(t) }
		}(pd)) // Capture pdService for cleanup
	}
	// Give the cluster time to elect a leader.
	time.Sleep(5 * time.Second) // Original e2e_test also has this sleep

	// Start Worker
	workerPort := GetFreePort()
	workerAddr := fmt.Sprintf("127.0.0.1:%d", workerPort)
	workerDataDir, err := os.MkdirTemp("", "worker_e2e_full_test-")
	require.NoError(t, err)
	defer os.RemoveAll(workerDataDir)
	worker, err := StartService(ctx, "worker",
		[]string{}, // No environment variables for worker
		"./bin/worker",
		fmt.Sprintf("--grpc-addr=%s", workerAddr),
		fmt.Sprintf("--pd-addrs=%s", allPdGrpcAddrs), // Pass all PD gRPC addrs
		fmt.Sprintf("--data-dir=%s", workerDataDir),
		"--node-id=1", // Assign a node ID
	)
	require.NoError(t, err)
	t.Cleanup(func() { worker.Stop(t) })

	// Start Reranker
	rerankerPort := GetFreePort()
	rerankerAddr := fmt.Sprintf("127.0.0.1:%d", rerankerPort)
	reranker, err := StartService(ctx, "reranker",
		[]string{
			"RULE_EXACT_MATCH_BOOST=0.3",
			"RULE_TITLE_BOOST=0.2",
			"RULE_METADATA_BOOSTS=verified:0.3,featured:0.2",
			"RULE_METADATA_PENALTIES=deprecated:0.5",
		},
		"./bin/reranker",
		fmt.Sprintf("--port=%d", rerankerPort), // Pass just the port number
		"--strategy=rule",
		"--cache=memory",
	)
	require.NoError(t, err)
	t.Cleanup(func() { reranker.Stop(t) })

	// Create temp directory for apigateway feedback database
	gatewayDataDir, err := os.MkdirTemp("", "gateway_e2e_full_test-")
	require.NoError(t, err)
	defer os.RemoveAll(gatewayDataDir)

	gatewayPort := GetFreePort()
	t.Logf("DEBUG: In test, gatewayPort obtained: %d", gatewayPort)
	gatewayAddr := fmt.Sprintf("127.0.0.1:%d", gatewayPort)

	gatewayEnv := []string{ // Environment variables for apigateway
		fmt.Sprintf("GRPC_ADDR=%s", gatewayAddr),
		fmt.Sprintf("PLACEMENT_DRIVER=%s", allPdGrpcAddrs), // Use allPdGrpcAddrs for PLACEMENT_DRIVER env var
		fmt.Sprintf("AUTH_SERVICE_ADDR=%s", authSvcAddr),
		fmt.Sprintf("RERANKER_SERVICE_ADDR=%s", rerankerAddr),
		fmt.Sprintf("FEEDBACK_DB_PATH=%s/feedback.db", gatewayDataDir), // Use temp directory for feedback DB
	}
	gateway, err := StartService(ctx, "apigateway",
		gatewayEnv,
		"./bin/apigateway", // Binary path
		// Command-line arguments (none needed, as they are configured via env vars)
	)
	require.NoError(t, err)
	t.Cleanup(func() { gateway.Stop(t) })

	// --- 3. Wait for Services to be Ready ---
	_, err = WaitForGrpcService(ctx, gatewayAddr, 30*time.Second)
	require.NoError(t, err)
	for _, addr := range pdGrpcAddrs {
		_, err = WaitForGrpcService(ctx, addr, 30*time.Second)
		require.NoError(t, err, fmt.Sprintf("Failed to connect to PD gRPC service at %s", addr))
	}
	_, err = WaitForGrpcService(ctx, workerAddr, 30*time.Second)
	require.NoError(t, err)
	_, err = WaitForGrpcService(ctx, rerankerAddr, 30*time.Second)
	require.NoError(t, err)
	log.Printf("DEBUG: Test client attempting to connect to APIGateway at %s", gatewayAddr)
	_, err = WaitForGrpcService(ctx, gatewayAddr, 30*time.Second)
	require.NoError(t, err)

	// --- 4. Get JWTs from Auth Service ---

	authClient, err := NewAuthClient(ctx, authSvcAddr)
	require.NoError(t, err)
	defer authClient.Close()

	userEmail := fmt.Sprintf("e2e-user-%s@example.com", uuid.New().String())
	userPassword := "e2e-password-strong!"

	// Register User (no authentication needed for registration)
	regResp, err := authClient.Client.RegisterUser(ctx, &authpb.RegisterUserRequest{
		Email:    userEmail,
		Password: userPassword,
	})
	require.NoError(t, err)
	require.NotNil(t, regResp.User)
	t.Logf("Successfully registered user: %s", userEmail)

	// Login to get Login JWT
	err = authClient.Login(ctx, userEmail, userPassword)
	require.NoError(t, err)
	t.Log("Successfully logged in and obtained Login JWT")

	// Create API Key using Login JWT (simulating frontend action)
	apiKeyName := "e2e-test-api-key"
	_, err = authClient.CreateAPIKey(authClient.NewAuthenticatedContext(ctx), apiKeyName)
	require.NoError(t, err)
	t.Logf("Successfully created API key: %s", apiKeyName)

	// List API keys to get the KeyPrefix
	listKeysResp, err := authClient.Client.ListAPIKeys(authClient.NewAuthenticatedContext(ctx), &authpb.ListAPIKeysRequest{})
	require.NoError(t, err)
	require.Len(t, listKeysResp.Keys, 1)
	testAPIKeyID := listKeysResp.Keys[0].KeyPrefix
	t.Logf("Obtained API Key ID: %s", testAPIKeyID)

	// Create SDK JWT using Login JWT and API Key ID
	sdkJwt, err := authClient.CreateSDKJWT(authClient.NewAuthenticatedContext(ctx), testAPIKeyID)
	require.NoError(t, err)
	require.NotEmpty(t, sdkJwt)
	t.Logf("Successfully obtained SDK JWT: %s...", sdkJwt[:10])

	// --- 5. Run Test Logic using Go Client with SDK JWT ---

	client, err := vectron.NewClient(gatewayAddr, sdkJwt) // Pass SDK JWT
	require.NoError(t, err)
	defer client.Close()

	collectionName := "e2e-full-test-collection"

	// 1. Create Collection
	err = client.CreateCollection(collectionName, 4, "euclidean")
	require.NoError(t, err)
	t.Logf("CreateCollection successful for collection: %s", collectionName)

	// It can take a moment for the collection and its shards to be fully initialized.

	// We poll the status until it's ready.

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

		t.Log("Collection is ready!")

		return true

	}, 20*time.Second, 1*time.Second, "timed out waiting for collection to be ready")

	// 2. List Collections

	collections, err := client.ListCollections()

	require.NoError(t, err)

	assert.Contains(t, collections, collectionName)

	t.Log("ListCollections successful")

	// 3. Upsert Points

	points := []*vectron.Point{

		{ID: "p1", Vector: []float32{0.11, 0.22, 0.33, 0.44}},

		{ID: "p2", Vector: []float32{0.55, 0.66, 0.77, 0.88}},

		{ID: "p3", Vector: []float32{0.12, 0.23, 0.34, 0.45}},
	}

	upserted, err := client.Upsert(collectionName, points)

	require.NoError(t, err)

	assert.Equal(t, int32(3), upserted)

	t.Logf("Upsert successful for %d points", upserted)

	// Allow a moment for the upserts to be indexed.

	time.Sleep(2 * time.Second)

	// 4. Search

	results, err := client.Search(collectionName, []float32{0.1, 0.2, 0.3, 0.4}, 2)

	require.NoError(t, err)

	require.Len(t, results, 2)

	assert.Equal(t, "p1", results[0].ID)

	assert.Equal(t, "p3", results[1].ID)

	t.Log("Search successful")

	// 5. Get Point

	point, err := client.Get(collectionName, "p2")

	require.NoError(t, err)

	assert.Equal(t, "p2", point.ID)

	assert.Equal(t, []float32{0.55, 0.66, 0.77, 0.88}, point.Vector)

	t.Logf("Get successful for point: %s", point.ID)

	// 6. Delete Point

	err = client.Delete(collectionName, "p1")

	require.NoError(t, err)

	t.Logf("Delete successful for point: p1")

	// 7. Get Deleted Point (should fail)

	_, err = client.Get(collectionName, "p1")

	assert.Error(t, err)

	assert.Contains(t, err.Error(), vectron.ErrNotFound.Error(), "expected 'not found' error when getting a deleted point")

	t.Log("Verified that getting a deleted point returns a 'not found' error")

}
