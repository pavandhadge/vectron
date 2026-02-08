# Auth Service (Current)

Last updated: 2026-02-08

## 1. What It Does

Auth Service manages identity and credentials for Vectron.

- user registration/login
- JWT session creation/validation
- user profile update/deletion
- token refresh
- API key create/list/delete
- API key validation for internal services
- SDK JWT creation

Storage backend: etcd.

## 2. Runtime Defaults, Env Loading, and Requirements

From `auth/service/cmd/auth/main.go`:

- gRPC: `GRPC_PORT` (default `:8081`)
- HTTP gateway: `HTTP_PORT` (default `:8082`)
- etcd endpoints: `ETCD_ENDPOINTS` (default `localhost:2379`)
- CORS origins: `CORS_ALLOWED_ORIGINS`

Hard requirement:

- `JWT_SECRET` must be set and length >= 32.
- Service exits on startup if this is not satisfied.

Env files are loaded on startup in this order (first match wins):
1. `.env.auth`
2. `auth.env`
3. `env/auth.env`

## 3. Security Model

- Passwords are hashed (bcrypt) before persistence.
- API keys are stored hashed; full key is returned once at creation.
- JWT middleware protects authenticated RPCs.
- Endpoint-specific rate limits exist for high-risk methods (login/register/key creation).

## 4. API Surface

Contract is `shared/proto/auth/auth.proto`.

User/account RPCs:

- `RegisterUser`, `Login`, `GetUserProfile`, `UpdateUserProfile`, `DeleteUser`, `RefreshToken`

API key RPCs:

- `CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`

Internal/SDK RPCs:

- `ValidateAPIKey`, `CreateSDKJWT`, `GetAuthDetailsForSDK`

## 5. Integration Points

- API Gateway calls `ValidateAPIKey` and SDK auth-detail RPCs.
- Auth frontend uses HTTP gateway routes for account and key management.

## 6. Source of Truth

- `auth/service/cmd/auth/main.go`
- `auth/service/internal/handler/auth.go`
- `shared/proto/auth/auth.proto`
