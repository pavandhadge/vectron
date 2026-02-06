module github.com/pavandhadge/vectron/apigateway

go 1.24.0

require (
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.7
	github.com/mattn/go-sqlite3 v1.14.24
	github.com/pavandhadge/vectron/shared v0.0.0
	github.com/redis/go-redis/v9 v9.6.1
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/pavandhadge/vectron/shared => ../shared

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
)
