// This file contains integration tests for the Auth gRPC service handler.
// It uses an embedded etcd server and an in-process gRPC server to test
// the handler's interaction with the persistence layer in an isolated environment.

package handler

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	etcdclient "github.com/pavandhadge/vectron/auth/service/internal/etcd"
	authpb "github.com/pavandhadge/vectron/auth/service/proto/auth"
)

const (
	testJWTSecret = "test-secret-for-auth-service"
	bufSize       = 1024 * 1024
)

var (
	etcd *embed.Etcd
	lis  *bufconn.Listener
)

// TestMain sets up and tears down the embedded etcd and gRPC servers.
func TestMain(m *testing.M) {
	var err error
	// Setup: Start an embedded etcd server
	cfg := embed.NewConfig()
	cfg.Dir = fmt.Sprintf("test-etcd-%s.etcd", uuid.New().String())
	etcd, err = embed.StartEtcd(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to start embedded etcd: %v", err))
	}
	select {
	case <-etcd.Server.ReadyNotify():
		// Ready
	case <-time.After(60 * time.Second):
		etcd.Server.Stop() // trigger a shutdown
		panic("etcd server took too long to start")
	}

	// Setup: Start an in-process gRPC server
	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer()

	// Create dependencies for the auth server
	etcdCli, err := etcdclient.NewClient([]string{etcd.Clients[0].Addr().String()}, 5*time.Second)
	if err != nil {
		panic(fmt.Sprintf("failed to connect to test etcd: %v", err))
	}
	authServer := NewAuthServer(etcdCli, testJWTSecret)
	authpb.RegisterAuthServiceServer(s, authServer)

	go func() {
		if err := s.Serve(lis); err != nil {
			panic(fmt.Sprintf("Server exited with error: %v", err))
		}
	}()

	// Run tests
	exitCode := m.Run()

	// Teardown
	s.GracefulStop()
	etcd.Close()
	os.RemoveAll(cfg.Dir)

	os.Exit(exitCode)
}

// newTestContext creates a context for a client call.
func newTestContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	return ctx
}

// newAuthenticatedContext creates a context with a valid JWT for an authenticated user.
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

// clearEtcd deletes all keys from the test etcd instance.
func clearEtcd(t *testing.T) {
	etcdCli, err := etcdclient.NewClient([]string{etcd.Clients[0].Addr().String()}, 5*time.Second)
	require.NoError(t, err)
	defer etcdCli.Close()
	_, err = etcdCli.Delete(context.Background(), "", clientv3.WithPrefix())
	require.NoError(t, err)
}

// TestAuthService runs a suite of integration tests for the auth service.
func TestAuthService(t *testing.T) {
	// Create a client to the in-process server
	ctx := newTestContext()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	client := authpb.NewAuthServiceClient(conn)

	// Ensure each test runs with a clean slate
	t.Cleanup(func() {
		clearEtcd(t)
	})

	t.Run("User Registration", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			clearEtcd(t)
			res, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "test@example.com", Password: "password123"})
			assert.NoError(t, err)
			assert.NotNil(t, res.GetUser())
			assert.Equal(t, "test@example.com", res.GetUser().GetEmail())
			assert.NotEmpty(t, res.GetUser().GetId())
		})
		t.Run("Duplicate Email", func(t *testing.T) {
			clearEtcd(t)
			// First registration should succeed
			_, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "duplicate@example.com", Password: "password123"})
			require.NoError(t, err)
			// Second one should fail
			_, err = client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "duplicate@example.com", Password: "password123"})
			assert.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.AlreadyExists, st.Code())
		})
		t.Run("Invalid Input", func(t *testing.T) {
			clearEtcd(t)
			_, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "", Password: "password123"})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())

			_, err = client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "test@test.com", Password: ""})
			assert.Error(t, err)
			st, _ = status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	})

	t.Run("User Login", func(t *testing.T) {
		clearEtcd(t)
		// Register a user first
		_, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "login@example.com", Password: "password123"})
		require.NoError(t, err)

		t.Run("Success", func(t *testing.T) {
			res, err := client.Login(newTestContext(), &authpb.LoginRequest{Email: "login@example.com", Password: "password123"})
			assert.NoError(t, err)
			assert.NotEmpty(t, res.GetJwtToken())
			assert.Equal(t, "login@example.com", res.GetUser().GetEmail())
		})
		t.Run("User Not Found", func(t *testing.T) {
			_, err := client.Login(newTestContext(), &authpb.LoginRequest{Email: "notfound@example.com", Password: "password123"})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.NotFound, st.Code())
		})
		t.Run("Incorrect Password", func(t *testing.T) {
			_, err := client.Login(newTestContext(), &authpb.LoginRequest{Email: "login@example.com", Password: "wrongpassword"})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())
		})
	})

	t.Run("User Profile", func(t *testing.T) {
		clearEtcd(t)
		// Register and get authenticated context
		_, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "profile@example.com", Password: "password123"})
		require.NoError(t, err)
		authedCtx := newAuthenticatedContext(t, client, "profile@example.com", "password123")

		t.Run("Success", func(t *testing.T) {
			res, err := client.GetUserProfile(authedCtx, &authpb.GetUserProfileRequest{})
			assert.NoError(t, err)
			assert.Equal(t, "profile@example.com", res.GetUser().GetEmail())
		})
		t.Run("Unauthenticated", func(t *testing.T) {
			// Use a context without auth metadata
			_, err := client.GetUserProfile(newTestContext(), &authpb.GetUserProfileRequest{})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())
		})
		t.Run("Invalid Token", func(t *testing.T) {
			// Use a context with a bad token
			badMd := metadata.Pairs("authorization", "Bearer bad-token")
			badCtx := metadata.NewOutgoingContext(newTestContext(), badMd)
			_, err := client.GetUserProfile(badCtx, &authpb.GetUserProfileRequest{})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())
		})
	})

	t.Run("API Key Management", func(t *testing.T) {
		clearEtcd(t)
		// Setup two users
		_, err := client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "user1@example.com", Password: "password123"})
		require.NoError(t, err)
		user1Ctx := newAuthenticatedContext(t, client, "user1@example.com", "password123")

		_, err = client.RegisterUser(newTestContext(), &authpb.RegisterUserRequest{Email: "user2@example.com", Password: "password123"})
		require.NoError(t, err)
		user2Ctx := newAuthenticatedContext(t, client, "user2@example.com", "password123")

		var user1KeyPrefix, user1FullKey string

		t.Run("Lifecycle", func(t *testing.T) {
			// Create a key for user1
			createRes, err := client.CreateAPIKey(user1Ctx, &authpb.CreateAPIKeyRequest{Name: "User 1 Key 1"})
			assert.NoError(t, err)
			assert.NotEmpty(t, createRes.GetFullKey())
			assert.Equal(t, "User 1 Key 1", createRes.GetKeyInfo().GetName())
			user1KeyPrefix = createRes.GetKeyInfo().GetKeyPrefix()
			user1FullKey = createRes.GetFullKey()

			// List keys for user1, should have 1 key
			listRes, err := client.ListAPIKeys(user1Ctx, &authpb.ListAPIKeysRequest{})
			assert.NoError(t, err)
			assert.Len(t, listRes.GetKeys(), 1)
			assert.Equal(t, user1KeyPrefix, listRes.GetKeys()[0].GetKeyPrefix())

			// List keys for user2, should have 0 keys
			listRes2, err := client.ListAPIKeys(user2Ctx, &authpb.ListAPIKeysRequest{})
			assert.NoError(t, err)
			assert.Len(t, listRes2.GetKeys(), 0)

			// Validate user1's key - should be valid
			validateRes, err := client.ValidateAPIKey(newTestContext(), &authpb.ValidateAPIKeyRequest{FullKey: user1FullKey})
			assert.NoError(t, err)
			assert.True(t, validateRes.GetValid())
			assert.Equal(t, "free", validateRes.GetPlan())
			user1Info, _ := client.GetUserProfile(user1Ctx, &authpb.GetUserProfileRequest{})
			assert.Equal(t, user1Info.GetUser().GetId(), validateRes.GetUserId())

			// Validate a fake key - should be invalid
			validateRes, err = client.ValidateAPIKey(newTestContext(), &authpb.ValidateAPIKeyRequest{FullKey: "vkey-this-is-a-fake-key"})
			assert.NoError(t, err)
			assert.False(t, validateRes.GetValid())

			// User2 tries to delete User1's key - should fail
			_, err = client.DeleteAPIKey(user2Ctx, &authpb.DeleteAPIKeyRequest{KeyPrefix: user1KeyPrefix})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.PermissionDenied, st.Code())

			// User1 deletes their own key - should succeed
			_, err = client.DeleteAPIKey(user1Ctx, &authpb.DeleteAPIKeyRequest{KeyPrefix: user1KeyPrefix})
			assert.NoError(t, err)

			// List keys for user1, should be empty now
			listRes, err = client.ListAPIKeys(user1Ctx, &authpb.ListAPIKeysRequest{})
			assert.NoError(t, err)
			assert.Len(t, listRes.GetKeys(), 0)

			// Validate the deleted key - should now be invalid
			validateRes, err = client.ValidateAPIKey(newTestContext(), &authpb.ValidateAPIKeyRequest{FullKey: user1FullKey})
			assert.NoError(t, err)
			assert.False(t, validateRes.GetValid())
		})

		t.Run("Unauthenticated Access", func(t *testing.T) {
			unauthedCtx := newTestContext()
			_, err := client.CreateAPIKey(unauthedCtx, &authpb.CreateAPIKeyRequest{Name: "test"})
			assert.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())

			_, err = client.ListAPIKeys(unauthedCtx, &authpb.ListAPIKeysRequest{})
			assert.Error(t, err)
			st, _ = status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())

			_, err = client.DeleteAPIKey(unauthedCtx, &authpb.DeleteAPIKeyRequest{KeyPrefix: "some-prefix"})
			assert.Error(t, err)
			st, _ = status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())
		})
	})
}
