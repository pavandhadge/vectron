package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pavandhadge/vectron/auth/service/testutil"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

const (
	testHTTPGatewayJWTSecret = "http-gateway-test-secret"
)

var (
	httpEtcdProc    *testutil.EtcdProcess
	httpAuthServer *testutil.AuthServerProcess
	httpTestCtx     context.Context
	httpTestCancel  context.CancelFunc
)

func TestMain(m *testing.M) {
	var err error
	httpTestCtx, httpTestCancel = context.WithCancel(context.Background())
	defer httpTestCancel()

	// 1. Start embedded etcd
	httpEtcdProc, err = testutil.StartEmbeddedEtcd(httpTestCtx)
	if err != nil {
		log.Fatalf("Failed to start embedded etcd for HTTP gateway test: %v", err)
	}
	defer httpEtcdProc.Stop()

	// 2. Start auth service instance with HTTP gateway
	httpAuthServer, err = testutil.StartAuthService(httpTestCtx, httpEtcdProc.ClientURL, testHTTPGatewayJWTSecret)
	if err != nil {
		log.Fatalf("Failed to start auth service for HTTP gateway test: %v", err)
	}
	defer httpAuthServer.Stop()

	// Wait for HTTP server to be ready (a simple poll for 200 OK on a known endpoint)
	err = waitForHTTPServerReady(httpTestCtx, fmt.Sprintf("http://127.0.0.1:%d/v1/users/register", httpAuthServer.HTTPPort))
	if err != nil {
		log.Fatalf("HTTP Gateway server not ready: %v", err)
	}

	log.Printf("HTTP Gateway server ready at port %d", httpAuthServer.HTTPPort)

	// Run tests
	exitCode := m.Run()

	os.Exit(exitCode)
}

func waitForHTTPServerReady(ctx context.Context, url string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for HTTP server at %s: %w", url, timeoutCtx.Err())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, url, bytes.NewBufferString(`{}`)) // A simple POST as /register is POST
			if err != nil {
				log.Printf("Error creating request for HTTP health check: %v", err)
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				// We expect an error response from /register endpoint as it needs email/password
				// but a successful connection means the server is up.
				resp.Body.Close()
				return nil
			}
			log.Printf("Waiting for HTTP server at %s: %v", url, err)
		}
	}
}

func makeHTTPRequest(t *testing.T, method, path string, body interface{}, authHeader string) *http.Response {
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", httpAuthServer.HTTPPort, path)

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	respBodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes)) // Restore body for later reading

	log.Printf("HTTP Response for %s %s (Status: %d): %s", method, path, resp.StatusCode, string(respBodyBytes))

	return resp
}

func TestHTTPGateway(t *testing.T) {

	t.Run("User Registration via HTTP", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			email := "http_register_user@example.com"
			password := "httppass123"
			registerReq := authpb.RegisterUserRequest{Email: email, Password: password}

			resp := makeHTTPRequest(t, http.MethodPost, "/v1/users/register", registerReq, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var res authpb.RegisterUserResponse
			err := json.NewDecoder(resp.Body).Decode(&res)
			assert.NoError(t, err)
			assert.NotEmpty(t, res.GetUser().GetId())
			assert.Equal(t, email, res.GetUser().GetEmail())
		})

		t.Run("Duplicate Email", func(t *testing.T) {
			email := "http_duplicate_user@example.com"
			password := "httppass123"
			registerReq := authpb.RegisterUserRequest{Email: email, Password: password}

			// First registration
			resp1 := makeHTTPRequest(t, http.MethodPost, "/v1/users/register", registerReq, "")
			defer resp1.Body.Close()
			assert.Equal(t, http.StatusOK, resp1.StatusCode)

			// Second registration with same email
			resp2 := makeHTTPRequest(t, http.MethodPost, "/v1/users/register", registerReq, "")
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusConflict, resp2.StatusCode) // gRPC code AlreadyExists maps to 409 Conflict
		})

		t.Run("Invalid Input", func(t *testing.T) {
			// Missing email
			resp1 := makeHTTPRequest(t, http.MethodPost, "/v1/users/register", authpb.RegisterUserRequest{Password: "pass"}, "")
			defer resp1.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp1.StatusCode) // gRPC code InvalidArgument maps to 400 Bad Request

			// Missing password
			resp2 := makeHTTPRequest(t, http.MethodPost, "/v1/users/register", authpb.RegisterUserRequest{Email: "a@b.com"}, "")
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
		})
	})

	t.Run("User Login via HTTP", func(t *testing.T) {
		email := "http_login_user@example.com"
		password := "httploginpass"
		registerReq := authpb.RegisterUserRequest{Email: email, Password: password}
		makeHTTPRequest(t, http.MethodPost, "/v1/users/register", registerReq, "") // Register first

		t.Run("Success", func(t *testing.T) {
			loginReq := authpb.LoginRequest{Email: email, Password: password}
			resp := makeHTTPRequest(t, http.MethodPost, "/v1/users/login", loginReq, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Manually unmarshal to handle camelCase from gRPC-Gateway
			var gwResp struct {
				JwtToken string `json:"jwtToken"`
				User     *authpb.User `json:"user"`
			}
			err := json.NewDecoder(resp.Body).Decode(&gwResp)
			assert.NoError(t, err)

			var res authpb.LoginResponse
			res.JwtToken = gwResp.JwtToken
			res.User = gwResp.User

			assert.NotEmpty(t, res.GetJwtToken())
			assert.Equal(t, email, res.GetUser().GetEmail())
		})

		t.Run("Invalid Credentials", func(t *testing.T) {
			loginReq := authpb.LoginRequest{Email: email, Password: "wrongpass"}
			resp := makeHTTPRequest(t, http.MethodPost, "/v1/users/login", loginReq, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode) // gRPC code Unauthenticated maps to 401 Unauthorized
		})

		t.Run("User Not Found", func(t *testing.T) {
			loginReq := authpb.LoginRequest{Email: "nonexistent@example.com", Password: "anypass"}
			resp := makeHTTPRequest(t, http.MethodPost, "/v1/users/login", loginReq, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode) // gRPC code NotFound maps to 404 Not Found
		})
	})

	t.Run("Authenticated Endpoints via HTTP", func(t *testing.T) {
		email := "http_authed_user@example.com"
		password := "httpauthedpass"
		registerReq := authpb.RegisterUserRequest{Email: email, Password: password}
		makeHTTPRequest(t, http.MethodPost, "/v1/users/register", registerReq, "") // Register first

		// Login to get a valid JWT token
		loginReq := authpb.LoginRequest{Email: email, Password: password}
		loginResp := makeHTTPRequest(t, http.MethodPost, "/v1/users/login", loginReq, "")
		defer loginResp.Body.Close()
		require.Equal(t, http.StatusOK, loginResp.StatusCode)
		var loginRes authpb.LoginResponse
		err := json.NewDecoder(loginResp.Body).Decode(&loginRes)
		require.NoError(t, err)
		jwtToken := loginRes.GetJwtToken()
		authHeader := fmt.Sprintf("Bearer %s", jwtToken)

		t.Run("GetUserProfile Success", func(t *testing.T) {
			resp := makeHTTPRequest(t, http.MethodGet, "/v1/user/profile", nil, authHeader)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var res authpb.GetUserProfileResponse
			err := json.NewDecoder(resp.Body).Decode(&res)
			assert.NoError(t, err)
			assert.Equal(t, email, res.GetUser().GetEmail())
		})

		t.Run("GetUserProfile Unauthorized", func(t *testing.T) {
			resp := makeHTTPRequest(t, http.MethodGet, "/v1/user/profile", nil, "") // No auth header
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})

		t.Run("CreateAPIKey Success", func(t *testing.T) {
			createKeyReq := authpb.CreateAPIKeyRequest{Name: "MyHttpApiKey"}
			resp := makeHTTPRequest(t, http.MethodPost, "/v1/keys", createKeyReq, authHeader)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var res authpb.CreateAPIKeyResponse
			err := json.NewDecoder(resp.Body).Decode(&res)
			assert.NoError(t, err)
			assert.NotEmpty(t, res.GetFullKey())
			assert.NotEmpty(t, res.GetKeyInfo().GetKeyPrefix())
			assert.Equal(t, "MyHttpApiKey", res.GetKeyInfo().GetName())
		})

		t.Run("ListAPIKeys Success", func(t *testing.T) {
			// Assuming a key was created in previous test
			resp := makeHTTPRequest(t, http.MethodGet, "/v1/keys", nil, authHeader)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var res authpb.ListAPIKeysResponse
			err := json.NewDecoder(resp.Body).Decode(&res)
			assert.NoError(t, err)
			assert.NotEmpty(t, res.GetKeys())
			assert.Equal(t, "MyHttpApiKey", res.GetKeys()[0].GetName()) // Check for the created key
		})

		t.Run("DeleteAPIKey Success", func(t *testing.T) {
			// First, create a key to delete
			createKeyReq := authpb.CreateAPIKeyRequest{Name: "KeyToDelete"}
			createResp := makeHTTPRequest(t, http.MethodPost, "/v1/keys", createKeyReq, authHeader)
			defer createResp.Body.Close()
			require.Equal(t, http.StatusOK, createResp.StatusCode)
			var createRes authpb.CreateAPIKeyResponse
			err := json.NewDecoder(createResp.Body).Decode(&createRes)
			require.NoError(t, err)
			keyPrefixToDelete := createRes.GetKeyInfo().GetKeyPrefix()

			// Now delete it
			resp := makeHTTPRequest(t, http.MethodDelete, fmt.Sprintf("/v1/keys/%s", keyPrefixToDelete), nil, authHeader)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify it's gone
			listResp := makeHTTPRequest(t, http.MethodGet, "/v1/keys", nil, authHeader)
			defer listResp.Body.Close()
			assert.Equal(t, http.StatusOK, listResp.StatusCode)
			var listRes authpb.ListAPIKeysResponse
			err = json.NewDecoder(listResp.Body).Decode(&listRes)
			assert.NoError(t, err)
			assert.Len(t, listRes.GetKeys(), 1) // Only "MyHttpApiKey" should remain
		})
	})
}