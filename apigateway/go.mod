module github.com/pavandhadge/vectron/apigateway

go 1.24.0

require (
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217
	google.golang.org/grpc v1.77.0
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.46.1-0.20251013234738-63d1a5100f82 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)


replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1

replace google.golang.org/genproto/googleapis/api => google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1

replace google.golang.org/genproto/googleapis/rpc => google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1
