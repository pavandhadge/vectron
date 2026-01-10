package service_test

import (
	"context"
	"log"
	"os"
	
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/pavandhadge/vectron/auth/service/testutil"
	authpb "github.com/pavandhadge/vectron/auth/service/proto/auth"
)

const (
	testDistributedJWTSecret = "super-secret-key-for-distributed-tests"
	numAuthServers           = 2
)

var (
	etcdProc   *testutil.EtcdProcess
	authServers []*testutil.AuthServerProcess
	ctx        context.Context
	cancel     context.CancelFunc
)

func TestMain(m *testing.M) {
	var err error
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// 1. Start embedded etcd
	etcdProc, err = testutil.StartEmbeddedEtcd(ctx)
	if err != nil {
		log.Fatalf("Failed to start embedded etcd: %v", err)
	}
	defer etcdProc.Stop()

	// 2. Start multiple auth service instances
	authServers = make([]*testutil.AuthServerProcess, numAuthServers)
	var wg sync.WaitGroup
	for i := 0; i < numAuthServers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			server, err := testutil.StartAuthService(ctx, etcdProc.ClientURL, testDistributedJWTSecret)
			if err != nil {
				log.Fatalf("Failed to start auth service instance %d: %v", index, err)
			}
			authServers[index] = server
		}(i)
	}
	wg.Wait()

	// Ensure all servers started successfully
	for i, server := range authServers {
		if server == nil {
			log.Fatalf("Auth server %d failed to start.", i)
		}
	}
	log.Printf("Successfully started %d auth service instances.", numAuthServers)

	// Run tests
	exitCode := m.Run()

	// Teardown: Stop auth services (deferred etcdProc.Stop() handles etcd)
	for _, server := range authServers {
		if server != nil {
			server.Stop()
		}
	}
	log.Println("Stopped all auth service instances.")


	os.Exit(exitCode)
}

// newTestContext creates a context for a client call with a timeout.
func newTestContext() context.Context {
	testCtx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	return testCtx
}

// newAuthenticatedContext creates a context with a valid JWT for an authenticated user
// by logging in through the specified auth client.
func newAuthenticatedContext(t *testing.T, client authpb.AuthServiceClient, email, password string) context.Context {
	ctx := newTestContext()
	// Login to get a token
	loginRes, err := client.Login(ctx, &authpb.LoginRequest{Email: email, Password: password})
	require.NoError(t, err)
	require.NotEmpty(t, loginRes.GetJwtToken())

	// Add token to metadata for subsequent calls
	md := metadata.Pairs("authorization", "Bearer "+loginRes.GetJwtToken())
	return metadata.NewOutgoingContext(ctx, md)
}

// TestDistributedAuthService checks distributed registration, login, and API key management.
func TestDistributedAuthService(t *testing.T) {
	require.Len(t, authServers, numAuthServers, "Expected %d auth servers to be running", numAuthServers)

	// Use Server 0 for initial registration
	client0 := authServers[0].Client
	// Use Server 1 for subsequent operations to test distribution
	client1 := authServers[1].Client

	t.Run("User Registration and Login across servers", func(t *testing.T) {
		email := "distributed_user@example.com"
		password := "distpass123"

		// Register user via Server 0
		regRes, err := client0.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: email, Password: password})
		assert.NoError(t, err)
		assert.NotNil(t, regRes.GetUser())
		assert.Equal(t, email, regRes.GetUser().GetEmail())

		// Login via Server 1 using user registered on Server 0
		loginRes, err := client1.Login(newTestContext(), &authpb.LoginRequest{Email: email, Password: password})
		assert.NoError(t, err)
		assert.NotEmpty(t, loginRes.GetJwtToken())
		assert.Equal(t, email, loginRes.GetUser().GetEmail())

		// Get profile via Server 0 (authenticated with token from Server 1)
		authedCtx0 := newAuthenticatedContext(t, client0, email, password)
		profileRes0, err := client0.GetUserProfile(authedCtx0, &authpb.GetUserProfileRequest{})
		assert.NoError(t, err)
		assert.Equal(t, email, profileRes0.GetUser().GetEmail())

		// Get profile via Server 1 (authenticated with token from Server 1)
		authedCtx1 := newAuthenticatedContext(t, client1, email, password)
		profileRes1, err := client1.GetUserProfile(authedCtx1, &authpb.GetUserProfileRequest{})
		assert.NoError(t, err)
		assert.Equal(t, email, profileRes1.GetUser().GetEmail())
	})

	t.Run("API Key Management across servers", func(t *testing.T) {
		email := "apikey_user@example.com"
		password := "keypass123"

		// Register user via Server 0
		_, err := client0.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: email, Password: password})
		require.NoError(t, err)

		// Get authenticated context from Server 0
		authedCtx0 := newAuthenticatedContext(t, client0, email, password)
		// Get authenticated context from Server 1
		authedCtx1 := newAuthenticatedContext(t, client1, email, password)

		// Create API key via Server 0
		createRes, err := client0.CreateAPIKey(authedCtx0, &authpb.CreateAPIKeyRequest{Name: "KeyFromS0"})
		assert.NoError(t, err)
		assert.NotEmpty(t, createRes.GetFullKey())
		createdKeyPrefix := createRes.GetKeyInfo().GetKeyPrefix()
		createdFullKey := createRes.GetFullKey()

		// List API keys via Server 1 - should see the key created by Server 0
		listRes1, err := client1.ListAPIKeys(authedCtx1, &authpb.ListAPIKeysRequest{})
		assert.NoError(t, err)
		assert.Len(t, listRes1.GetKeys(), 1)
		assert.Equal(t, createdKeyPrefix, listRes1.GetKeys()[0].GetKeyPrefix())
		assert.Equal(t, "KeyFromS0", listRes1.GetKeys()[0].GetName())

		// Validate API key via Server 1 - should be valid
		validateRes1, err := client1.ValidateAPIKey(newTestContext(), &authpb.ValidateAPIKeyRequest{FullKey: createdFullKey})
		assert.NoError(t, err)
		assert.True(t, validateRes1.GetValid())

		// Delete API key via Server 1
		_, err = client1.DeleteAPIKey(authedCtx1, &authpb.DeleteAPIKeyRequest{KeyPrefix: createdKeyPrefix})
		assert.NoError(t, err)

		// List API keys via Server 0 - should now be empty
		listRes0, err := client0.ListAPIKeys(authedCtx0, &authpb.ListAPIKeysRequest{})
		assert.NoError(t, err)
		assert.Len(t, listRes0.GetKeys(), 0)

		// Validate deleted key via Server 0 - should be invalid
		validateRes0, err := client0.ValidateAPIKey(newTestContext(), &authpb.ValidateAPIKeyRequest{FullKey: createdFullKey})
		assert.NoError(t, err)
		assert.False(t, validateRes0.GetValid())
	})

	t.Run("Error handling - Unauthorized access", func(t *testing.T) {
		// Use server 0 to try and access API keys without authentication
		_, err := client0.ListAPIKeys(newTestContext(), &authpb.ListAPIKeysRequest{})
		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}
