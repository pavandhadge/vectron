import (
	"context"
	"log"
	"net/http"

	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	pb "github.com/pavandhadge/vectron/apigateway/proto/apigateway"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

var cfg = LoadConfig()

func main() {
	middleware.SetJWTSecret(cfg.JWTSecret)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				EmitUnpopulated: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
	)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	if err := pb.RegisterVectronServiceHandlerFromEndpoint(
		ctx, mux, cfg.PlacementDriver, opts,
	); err != nil {
		log.Fatalf("Failed to connect to placement driver: %v", err)
	}

	handler := middleware.Logging(
		middleware.RateLimit(cfg.RateLimitRPS)(
			middleware.Auth(mux),
		),
	)

	log.Printf("Vectron API Gateway starting on %s", cfg.ListenAddr)
	log.Printf("Forwarding to placement driver at %s", cfg.PlacementDriver)

	log.Fatal(http.ListenAndServe(cfg.ListenAddr, handler))
}
