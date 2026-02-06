package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/pavandhadge/vectron/shared/runtimeutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"

	etcdclient "github.com/pavandhadge/vectron/auth/service/internal/etcd"
	authhandler "github.com/pavandhadge/vectron/auth/service/internal/handler"
	"github.com/pavandhadge/vectron/auth/service/internal/middleware"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

// splitOrigins splits a comma-separated list of origins
func splitOrigins(origins string) []string {
	var result []string
	for _, o := range strings.Split(origins, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			result = append(result, o)
		}
	}
	return result
}

var (
	grpcPort      string = ":8081"
	httpPort      string = ":8082"
	etcdEndpoints string = "localhost:2379"
	jwtSecret     string
)

func init() {
	if os.Getenv("GRPC_PORT") != "" {
		grpcPort = os.Getenv("GRPC_PORT")
	}
	if os.Getenv("HTTP_PORT") != "" {
		httpPort = os.Getenv("HTTP_PORT")
	}
	if os.Getenv("ETCD_ENDPOINTS") != "" {
		etcdEndpoints = os.Getenv("ETCD_ENDPOINTS")
	}

	// JWT_SECRET is REQUIRED for production. The service will fail to start without it.
	// In production, this should be loaded from a secure secret management system
	// (e.g., HashiCorp Vault, AWS Secrets Manager, Kubernetes Secrets, etc.)
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatalf("FATAL: JWT_SECRET environment variable is required but not set. " +
			"Please set a secure JWT secret (min 32 characters) before starting the service. " +
			"Example: export JWT_SECRET=$(openssl rand -base64 32)")
	}
	if len(jwtSecret) < 32 {
		log.Fatalf("FATAL: JWT_SECRET must be at least 32 characters long for security. Current length: %d", len(jwtSecret))
	}
}

func main() {
	runtimeutil.ConfigureGOMAXPROCS("auth")
	// Initialize etcd client
	etcdCli, err := etcdclient.NewClient([]string{etcdEndpoints}, 5*time.Second)
	if err != nil {
		log.Fatalf("failed to connect to etcd: %v", err)
	}
	defer etcdCli.Close()

	// Start gRPC server with middleware
	go func() {
		if err := runGrpcServer(etcdCli); err != nil {
			log.Fatalf("failed to run gRPC server: %v", err)
		}
	}()

	// Start gRPC-Gateway (HTTP server)
	if err := runHttpServer(); err != nil {
		log.Fatalf("failed to run HTTP server: %v", err)
	}
}

func runGrpcServer(store *etcdclient.Client) error {
	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		return err
	}

	// Define which RPCs do not require JWT authentication
	authInterceptor := middleware.NewAuthInterceptor(jwtSecret, []string{
		"/vectron.auth.v1.AuthService/RegisterUser",
		"/vectron.auth.v1.AuthService/Login",
		"/vectron.auth.v1.AuthService/ValidateAPIKey",
		"/vectron.auth.v1.AuthService/GetAuthDetailsForSDK",
	})

	// Setup rate limiting for auth endpoints
	rateLimiter := middleware.NewRateLimiter()
	// Strict rate limits for auth endpoints to prevent brute force
	rateLimiter.SetLimit("/vectron.auth.v1.AuthService/Login", 5, time.Minute)         // 5 login attempts per minute
	rateLimiter.SetLimit("/vectron.auth.v1.AuthService/RegisterUser", 3, time.Minute)  // 3 registrations per minute
	rateLimiter.SetLimit("/vectron.auth.v1.AuthService/CreateAPIKey", 10, time.Minute) // 10 API keys per minute
	rateLimiter.SetLimit("/vectron.auth.v1.AuthService/CreateSDKJWT", 10, time.Minute) // 10 SDK JWTs per minute
	go rateLimiter.Cleanup(5 * time.Minute)                                            // Cleanup old entries every 5 minutes

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			rateLimiter.Unary(),
			authInterceptor.Unary(),
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ReadBufferSize(64*1024),
		grpc.WriteBufferSize(64*1024),
		grpc.MaxConcurrentStreams(1024),
	)

	authServer := authhandler.NewAuthServer(store, jwtSecret)
	authpb.RegisterAuthServiceServer(s, authServer)
	log.Printf("gRPC server listening at %v", lis.Addr())
	return s.Serve(lis)
}

func runHttpServer() error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                20 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithInitialWindowSize(1 << 20),
		grpc.WithInitialConnWindowSize(1 << 20),
		grpc.WithReadBufferSize(64 * 1024),
		grpc.WithWriteBufferSize(64 * 1024),
	}

	if err := authpb.RegisterAuthServiceHandlerFromEndpoint(ctx, mux, "localhost"+grpcPort, opts); err != nil {
		return err
	}

	// Production CORS middleware - configured via environment variables
	// By default, only allows same-origin requests for security
	allowedOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		// Default: same-origin only for production security
		allowedOrigins = "http://localhost:10011,http://127.0.0.1:10011"
	}

	corsHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			isAllowed := false
			if allowedOrigins == "*" {
				// Wildcard is allowed but ONLY if credentials are not used
				isAllowed = true
			} else {
				for _, allowed := range splitOrigins(allowedOrigins) {
					if origin == allowed || allowed == "*" {
						isAllowed = true
						break
					}
				}
			}

			if isAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Content-Length, X-CSRF-Token, X-API-Key")
			// Credentials are only allowed with specific origins, not with wildcard
			if allowedOrigins != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	log.Printf("HTTP server listening at %s", httpPort)
	return http.ListenAndServe(httpPort, corsHandler(mux))
}
