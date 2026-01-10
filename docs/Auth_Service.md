# Vectron Auth Service Documentation

This document provides a detailed analysis of the Vectron `auth` service.

## 1. Overview

The `auth` service is a foundational microservice within the Vectron ecosystem responsible for user identity and access management. It is a self-contained gRPC application that uses `etcd` for data persistence. It handles user registration, login, and the issuance and validation of API keys.

### 1.1. Core Responsibilities

*   **User Account Management:** Provides RPCs for user registration (`RegisterUser`) and login (`Login`).
*   **Session Management:** Upon successful login, it generates a JSON Web Token (JWT) that serves as a temporary session token for a user.
*   **API Key Management:** Provides authenticated RPCs for users to create, list, and delete API keys.
*   **API Key Validation:** Exposes an internal RPC (`ValidateAPIKey`) used by the `apigateway` to validate API keys from clients.

## 2. Architecture

The service exposes both a gRPC interface for internal service-to-service communication and a RESTful HTTP/JSON interface for use by the web frontend (which is out of scope for this documentation).

### 2.1. Data Persistence (etcd)

The service uses `etcd` as its backing data store. All data is serialized to JSON before being stored.

*   **Key Schema:**
    *   **Users:** Stored under the prefix `vectron/users/`, with the user's email as the key (e.g., `vectron/users/test@example.com`).
    *   **API Keys:** Stored under the prefix `vectron/apikeys/`, with the key's prefix as the key (e.g., `vectron/apikeys/vkey-xxxxxxxx`).

*   **Data Models:**
    *   `UserData`: Contains the user's ID, email, hashed password, and creation timestamp.
    *   `APIKeyData`: Contains the hashed full API key, the associated user ID, a descriptive name, the user's plan (e.g., "free"), and the key's prefix.

### 2.2. Security Model

Security is a primary design consideration for the auth service.

*   **Password Hashing:** User passwords are never stored in plain text. They are hashed using the `bcrypt` algorithm.
*   **API Key Hashing & Validation:**
    *   When a new API key is created, the service immediately hashes it with `bcrypt`.
    *   The record in `etcd` stores this **hashed key**. The full, plain text key is returned **only once** to the user upon creation.
    *   The first part of the key serves as a non-secret "key prefix", which is used as the key in `etcd`.
    *   To validate an API key, the `apigateway` sends the full key to the `ValidateAPIKey` RPC. The auth service uses the prefix to look up the record in `etcd` and then uses `bcrypt.CompareHashAndPassword` to securely verify the full key against the stored hash.
*   **Authentication Middleware:**
    *   The service uses a gRPC interceptor to protect its own endpoints.
    *   Most RPCs require a valid JWT (obtained via the `Login` RPC).
    *   The interceptor validates the JWT and extracts the `UserID` claim.
    *   A whitelist exempts `RegisterUser`, `Login`, and the internal `ValidateAPIKey` RPCs from this JWT check.

## 3. API Definition (from `shared/proto/auth/auth.proto`)

The gRPC and HTTP API for the `AuthService` is defined in the `auth.proto` file.

```protobuf
syntax = "proto3";

package vectron.auth.v1;

import "google/api/annotations.proto";

option go_package = "github.com/pavandhadge/vectron/shared/proto/auth";

// ================== Service Definition ==================

// AuthService provides user account and API key management.
service AuthService {
  // --- User Account Management ---

  // Register a new user.
  rpc RegisterUser(RegisterUserRequest) returns (RegisterUserResponse) {
    option (google.api.http) = {
      post: "/v1/users/register"
      body: "*"
    };
  }

  // Login a user and get a session JWT.
  rpc Login(LoginRequest) returns (LoginResponse) {
    option (google.api.http) = {
      post: "/v1/users/login"
      body: "*"
    };
  }

  // Get the current user's profile (requires JWT authentication).
  rpc GetUserProfile(GetUserProfileRequest) returns (GetUserProfileResponse) {
    option (google.api.http) = {get: "/v1/user/profile"};
  }

  // --- API Key Management (requires JWT authentication) ---

  // Create a new API key for the authenticated user.
  rpc CreateAPIKey(CreateAPIKeyRequest) returns (CreateAPIKeyResponse) {
    option (google.api.http) = {
      post: "/v1/keys"
      body: "*"
    };
  }

  // List all API keys for the authenticated user.
  rpc ListAPIKeys(ListAPIKeysRequest) returns (ListAPIKeysResponse) {
    option (google.api.http) = {get: "/v1/keys"};
  }

  // Delete an API key belonging to the authenticated user.
  rpc DeleteAPIKey(DeleteAPIKeyRequest) returns (DeleteAPIKeyResponse) {
    option (google.api.http) = {delete: "/v1/keys/{key_prefix}"};
  }

  // --- For Internal Services (e.g., API Gateway) ---

  // Validate a raw API key.
  rpc ValidateAPIKey(ValidateAPIKeyRequest) returns (ValidateAPIKeyResponse) {
    option (google.api.http) = {
      post: "/v1/keys/validate"
      body: "*"
    };
  }
}

// ================== Messages ==================

message User {
  string id = 1;
  string email = 2;
  int64 created_at = 3;
}

message APIKey {
  string key_prefix = 1;
  string user_id = 2;
  int64 created_at = 3;
  string name = 4;
}

// --- User Registration ---
message RegisterUserRequest {
  string email = 1;
  string password = 2;
}

message RegisterUserResponse {
  User user = 1;
}

// --- User Login ---
message LoginRequest {
  string email = 1;
  string password = 2;
}

message LoginResponse {
  string jwt_token = 1 [json_name = "jwtToken"];
  User user = 2;
}

// --- User Profile ---
message GetUserProfileRequest {} // User ID is extracted from JWT

message GetUserProfileResponse {
  User user = 1;
}

// --- API Key Management ---
message CreateAPIKeyRequest {
  // User ID is extracted from JWT
  string name = 1;
}

message CreateAPIKeyResponse {
  string full_key = 1; // The full key is only returned on creation
  APIKey key_info = 2;
}

message ListAPIKeysRequest {} // User ID is extracted from JWT

message ListAPIKeysResponse {
  repeated APIKey keys = 1;
}

message DeleteAPIKeyRequest {
  // User ID is extracted from JWT
  string key_prefix = 1;
}

message DeleteAPIKeyResponse {
  bool success = 1;
}

// --- Internal Validation ---
message ValidateAPIKeyRequest {
  string full_key = 1;
}

message ValidateAPIKeyResponse {
  bool valid = 1;
  string user_id = 2;
  string plan = 3;
  string api_key_id = 4;
}
```


## 4. Service Entrypoint (`cmd/auth/main.go`)

The `main.go` file is the entry point for the `auth` service. It is responsible for initializing and running all the components of the service.

### 4.1. Configuration

The service is configured through environment variables. Default values are used if the environment variables are not set.

| Environment Variable | Description                                    | Default         |
| -------------------- | ---------------------------------------------- | --------------- |
| `GRPC_PORT`          | The port for the gRPC server.                  | `:8081`         |
| `HTTP_PORT`          | The port for the HTTP/JSON gateway.            | `:8082`         |
| `ETCD_ENDPOINTS`     | Comma-separated list of etcd endpoints.        | `localhost:2379`|
| `JWT_SECRET`         | The secret key for signing and verifying JWTs. | (Generated at runtime if not set) |

A critical detail is the handling of `JWT_SECRET`. If this variable is not set, the application generates a temporary, cryptographically random key at startup. While this allows the service to run out-of-the-box, a warning is logged, as this is insecure and not suitable for production environments.

### 4.2. Initialization (`main` function)

The `main` function orchestrates the startup of the service:

1.  **Initialize etcd Client:** It creates a new client for `etcd` using the configured endpoints. If the connection fails, the service will not start.
2.  **Start gRPC Server:** It launches the gRPC server in a separate goroutine.
3.  **Start HTTP Server:** It starts the HTTP/JSON gateway in the main goroutine. The program will block here, and the service will exit if the HTTP server fails.

### 4.3. gRPC Server (`runGrpcServer`)

This function sets up and runs the gRPC server.

1.  **Listen on TCP Port:** It starts listening on the configured `GRPC_PORT`.
2.  **Instantiate Middleware:** It creates an instance of `AuthInterceptor` from the `middleware` package. A whitelist of RPC method paths is provided to the interceptor, defining which methods are exempt from JWT authentication. These are the public-facing and internal validation methods:
    *   `/vectron.auth.v1.AuthService/RegisterUser`
    *   `/vectron.auth.v1.AuthService/Login`
    *   `/vectron.auth.v1.AuthService/ValidateAPIKey`
3.  **Create gRPC Server:** A new gRPC server is created with the `AuthInterceptor` configured as a `UnaryInterceptor`. This means the interceptor will be executed for every unary (non-streaming) RPC call.
4.  **Register Service:** An instance of the `AuthServer` from the `handler` package is created and registered with the gRPC server. This is where the actual business logic of the service resides.
5.  **Serve:** The server starts accepting and processing incoming gRPC connections.

### 4.4. HTTP Server (`runHttpServer`)

This function sets up and runs the gRPC-Gateway, which translates RESTful HTTP/JSON requests into gRPC.

1.  **Create ServeMux:** It creates a new `runtime.NewServeMux()`, which is the gRPC-Gateway's request router.
2.  **Register Handler:** It calls `authpb.RegisterAuthServiceHandlerFromEndpoint`. This function, generated by `protoc-gen-grpc-gateway`, is the core of the gateway. It registers the HTTP handlers for the `AuthService` and configures them to proxy requests to the gRPC server endpoint (`localhost` on `GRPC_PORT`).
3.  **Listen and Serve:** It starts a standard Go HTTP server on the configured `HTTP_PORT`, using the gRPC-Gateway's mux as the handler.


## 5. gRPC Handlers (`internal/handler/auth.go`)

The `auth.go` file contains the implementation of the `AuthServiceServer` interface. This is where the core business logic of the service resides.

### 5.1. `AuthServer` Struct

The `AuthServer` struct is the receiver for all the gRPC method implementations.

```go
type AuthServer struct {
	authpb.UnimplementedAuthServiceServer
	store     *etcdclient.Client
	jwtSecret []byte
}
```

*   `authpb.UnimplementedAuthServiceServer`: It embeds this struct to ensure forward compatibility. If new RPCs are added to the `.proto` file, the service will still compile.
*   `store`: A pointer to the `etcdclient.Client`, which is used for all database operations.
*   `jwtSecret`: A byte slice containing the secret key used for signing and verifying JWTs.

A `NewAuthServer` function is used to create a new instance of the `AuthServer`.

### 5.2. RPC Implementations

#### `RegisterUser`
This method handles new user registration.
1.  It performs basic validation to ensure the email and password are not empty.
2.  It calls `s.store.CreateUser()`, delegating the core logic of hashing the password and storing the new user in `etcd`.
3.  It handles potential errors from the data layer, such as a user with the same email already existing.
4.  It returns a `RegisterUserResponse` containing the details of the newly created user.

#### `Login`
This method handles user authentication and session creation.
1.  It retrieves the user from `etcd` by their email address using `s.store.GetUserByEmail()`.
2.  If the user is found, it uses `bcrypt.CompareHashAndPassword()` to securely compare the hash of the provided password with the hash stored in `etcd`.
3.  Upon successful validation, it generates a JSON Web Token (JWT).
    *   The JWT claims include the `UserID` and are set to expire in 24 hours.
    *   The token is signed using the HMAC-SHA256 (`HS256`) algorithm and the `jwtSecret`.
4.  It returns a `LoginResponse` containing the signed JWT and the user's profile information.

#### `GetUserProfile`
This method is protected by the authentication middleware and provides the profile of the currently logged-in user.
1.  It calls `middleware.GetUserIDFromContext()` to extract the `UserID` from the request context. This `UserID` was previously validated and injected by the `AuthInterceptor`.
2.  It uses the `UserID` to fetch the user's data from `etcd` via `s.store.GetUserByID()`.
3.  It returns a `GetUserProfileResponse` with the user's details.

#### API Key Management (`CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`)
These methods are also protected by the authentication middleware.
1.  Like `GetUserProfile`, they all start by extracting the `UserID` from the request context.
2.  They delegate the core logic to the corresponding methods in the `etcd` client (`s.store`):
    *   `CreateAPIKey` calls `s.store.CreateAPIKey()`.
    *   `ListAPIKeys` calls `s.store.ListAPIKeys()`.
    *   `DeleteAPIKey` calls `s.store.DeleteAPIKey()`, passing both the key prefix to delete and the `UserID` to ensure a user can only delete their own keys.
3.  They return the appropriate response or an error. Notably, `CreateAPIKey` returns the full, unhashed API key, which is the only time the client will see it.

#### `ValidateAPIKey`
This is an internal-facing RPC intended for use by other services like the `apigateway`.
1.  It receives a full API key in the request.
2.  It calls `s.store.ValidateAPIKey()`, which contains the logic to look up the key by its prefix and perform a secure comparison using `bcrypt`.
3.  If the key is valid, it returns a `ValidateAPIKeyResponse` with `valid` set to `true`, along with the associated `UserID`, `Plan`, and `ApiKeyId` (the key prefix). If invalid, it returns `valid` as `false`.


## 6. Data Layer (`internal/etcd/client.go`)

The `etcd/client.go` file provides the data access layer for the `auth` service. It encapsulates all interactions with the `etcd` database, including data serialization, persistence, and the core security operations of hashing and comparison.

### 6.1. Client and Data Models

*   **`Client` Struct:** A simple struct that wraps the official `go-etcd/clientv3.Client`, providing a dedicated namespace for the service's database methods.
*   **`UserData` and `APIKeyData` Structs:** These are the Go representations of the data stored in `etcd`. They are serialized into JSON before being written to the database.
    *   `UserData`: `{id, email, hashed_password, created_at}`
    *   `APIKeyData`: `{hashed_key, user_id, name, plan, created_at, key_prefix}`

### 6.2. Key Schema

All keys are stored under a common `vectron/` prefix to namespace the application's data within `etcd`.

*   **User Key:** `vectron/users/{email}`
*   **API Key:** `vectron/apikeys/{key_prefix}`

### 6.3. User Management Methods

#### `CreateUser`
1.  **Pre-check:** It first attempts to fetch a user by the given email to prevent duplicates.
2.  **Hashing:** It uses `bcrypt.GenerateFromPassword` with the default cost to securely hash the plain-text password.
3.  **ID Generation:** It generates a new unique ID for the user (e.g., `user-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`).
4.  **Persistence:** It marshals the `UserData` struct to JSON and performs a `Put` operation to store it in `etcd` using the user's email as part of the key.

#### `GetUserByEmail`
This is an efficient operation. It constructs the full `etcd` key from the user's email and performs a direct `Get` request.

#### `GetUserByID`
**Performance Warning:** This is an inefficient operation. Since the user ID is not part of the `etcd` key, a direct lookup is not possible. The implementation must perform a **scan** of all documents under the `vectron/users/` prefix. It then iterates through the results in-memory, unmarshals each one, and compares the `ID` field. The comments in the code explicitly acknowledge this inefficiency, which can lead to performance degradation as the number of users increases.

### 6.4. API Key Management Methods

#### `CreateAPIKey`
This method implements the core security logic for API keys.
1.  **Generation:** It generates a new unique API key, prefixed with `vkey-`.
2.  **Hashing:** It immediately hashes the full, plain-text key using `bcrypt.GenerateFromPassword`.
3.  **Prefix Extraction:** It creates a non-secret `KeyPrefix` by taking the first 13 characters of the full key (e.g., `vkey-` plus the first 8 characters of the UUID). This prefix will serve as the lookup key in `etcd`.
4.  **Persistence:** It marshals the `APIKeyData` struct—which contains the **hashed key**—and stores it in `etcd` using the `KeyPrefix`.
5.  **Return Value:** It returns the **full, plain-text API key** to the handler. This is the only time this key is exposed.

#### `ValidateAPIKey`
This method provides a secure and efficient way to validate an API key.
1.  **Prefix Extraction:** It extracts the `KeyPrefix` from the full API key provided by the client.
2.  **Lookup:** It performs a direct `Get` request to `etcd` using the `KeyPrefix`, which is a highly efficient operation.
3.  **Secure Comparison:** If a key is found, it uses `bcrypt.CompareHashAndPassword` to compare the hash of the provided full key with the `HashedKey` retrieved from `etcd`. This comparison is done in constant time to prevent timing attacks.
4.  **Return Value:** It returns the `APIKeyData` and a boolean indicating the validity of the key.

#### `ListAPIKeys`
**Performance Warning:** Similar to `GetUserByID`, this is an inefficient scan operation. It fetches all keys under the `vectron/apikeys/` prefix and filters them in-memory by `UserID`. This will become slow as the total number of API keys in the system grows.

#### `DeleteAPIKey`
1.  **Lookup:** It fetches the key data from `etcd` using the key prefix.
2.  **Ownership Check:** It performs a critical security check to ensure the `UserID` from the key data matches the `UserID` of the user making the request. This prevents a user from deleting another user's keys.
3.  **Deletion:** If the check passes, it issues a `Delete` command to `etcd` to remove the key.

