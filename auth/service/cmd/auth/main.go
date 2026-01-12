package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	etcdclient "github.com/pavandhadge/vectron/auth/service/internal/etcd"
	authhandler "github.com/pavandhadge/vectron/auth/service/internal/handler"
	"github.com/pavandhadge/vectron/auth/service/internal/middleware"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

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

	// In a real production environment, this should be loaded from a secure
	// configuration management system (e.g., Vault, AWS KMS, etc.), not generated on the fly.
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Println("WARNING: JWT_SECRET environment variable not set. Using a temporary, insecure secret.")
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			log.Fatalf("Failed to generate random key for JWT secret: %v", err)
		}
		jwtSecret = hex.EncodeToString(key)
	}
}

func main() {
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

	s := grpc.NewServer(
		grpc.UnaryInterceptor(authInterceptor.Unary()),
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
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	if err := authpb.RegisterAuthServiceHandlerFromEndpoint(ctx, mux, "localhost"+grpcPort, opts); err != nil {
		return err
	}

	log.Printf("HTTP server listening at %s", httpPort)
	return http.ListenAndServe(httpPort, mux)
}
