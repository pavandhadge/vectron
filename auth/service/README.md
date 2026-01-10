# Vectron Auth Service

The `auth` service is a foundational microservice within the Vectron ecosystem responsible for user identity and access management. It handles user registration, login, and the issuance of API keys that are used to authenticate requests to the main API Gateway.

## Architecture and Design

The service is a self-contained gRPC application that uses `etcd` for data persistence. It exposes both a gRPC interface for internal service-to-service communication and a RESTful HTTP/JSON interface for use by the web frontend.

### 1. Core Responsibilities

- **User Account Management:** Provides RPCs for user registration (`RegisterUser`) and login (`Login`).
- **Session Management:** Upon successful login, it generates a JSON Web Token (JWT) that serves as a temporary session token for a user interacting with the management frontend.
- **API Key Management:** Provides authenticated RPCs for users to create, list, and delete API keys (`CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`).
- **API Key Validation:** Exposes an internal RPC (`ValidateAPIKey`) used by the `apigateway` to validate API keys presented by clients making vector database requests.

### 2. Data Persistence (etcd)

The service uses `etcd` as its backing data store. All data is serialized to JSON before being stored.

- **Key Schema:**
  - **Users:** Stored under the prefix `vectron/users/`, with the user's email as the key (e.g., `vectron/users/test@example.com`).
  - **API Keys:** Stored under the prefix `vectron/apikeys/`, with the key's prefix as the key (e.g., `vectron/apikeys/vkey-xxxxxxxx`).

- **Data Models:**
  - `UserData`: Contains the user's ID, email, hashed password, and creation timestamp.
  - `APIKeyData`: Contains the hashed full API key, the associated user ID, a descriptive name, the user's plan (e.g., "free"), and the key's prefix.

### 3. Security Model

Security is a primary design consideration for the auth service.

- **Password Hashing:** User passwords are never stored in plain text. They are hashed using the `bcrypt` algorithm before being persisted in `etcd`. When a user logs in, the provided password is hashed and compared to the stored hash.

- **API Key Hashing & Validation:**
  - When a new API key is created (e.g., `vkey-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`), the service immediately hashes it with `bcrypt`.
  - The record in `etcd` stores this **hashed key**. The full, plain text key is returned **only once** to the user upon creation.
  - The first part of the key (`vkey-xxxxxxxx`) serves as a non-secret "key prefix". This prefix is used as the key in `etcd`.
  - To validate an API key, the `apigateway` sends the full key to the `ValidateAPIKey` RPC. The auth service extracts the prefix, looks up the record in `etcd`, and then uses `bcrypt.CompareHashAndPassword` to securely verify that the provided full key matches the stored hash. This prevents the need to store sensitive API keys in plain text, drastically improving the security posture.

- **Authentication Middleware:**
  - The service uses a gRPC interceptor to protect its own endpoints.
  - RPCs like `CreateAPIKey` and `GetUserProfile` require a valid JWT (obtained via the `Login` RPC) to be present in the request headers.
  - The interceptor validates the JWT and extracts the `UserID` claim, which is then used by the handlers to ensure a user can only access their own data.
  - A whitelist exempts the `RegisterUser`, `Login`, and internal `ValidateAPIKey` RPCs from this JWT check.

## API Definition

The service's API is formally defined in `proto/auth/auth.proto`.

### User Management RPCs
- `RegisterUser`: Creates a new user account.
- `Login`: Authenticates a user and returns a session JWT.
- `GetUserProfile`: Retrieves the profile of the currently logged-in user.

### API Key Management RPCs
- `CreateAPIKey`: Creates a new API key for the authenticated user.
- `ListAPIKeys`: Lists all API keys belonging to the authenticated user.
- `DeleteAPIKey`: Deletes one of the authenticated user's API keys.

### Internal RPCs
- `ValidateAPIKey`: Validates a full API key and returns its validity status, associated UserID, and plan. This is intended to be called by other Vectron services like the API Gateway.

## Configuration

| Environment Variable | Description                                             | Default                                       |
| -------------------- | ------------------------------------------------------- | --------------------------------------------- |
| `JWT_SECRET`         | The secret key used to sign and verify session JWTs.    | A temporary, insecure key is generated at runtime if not set. |
| (Hardcoded)          | etcd endpoints                                          | `localhost:2379`                              |
| (Hardcoded)          | gRPC server port                                        | `:8081`                                       |
| (Hardcoded)          | HTTP/JSON gateway port                                  | `:8082`                                       |

---
This README provides a detailed overview of the `auth/service`. The next step is to analyze the `auth/frontend`.
