# Vectron Authentication System

The `auth` directory contains all components related to user identity and access management for the Vectron ecosystem. It is composed of two main parts: a backend **Auth Service** and a web-based **Frontend**.

## System Overview

The Vectron authentication system is responsible for two distinct but related authentication flows:

1.  **User Authentication:** Authenticating a human user who is managing their account and API keys. This is handled via a traditional email/password login which grants a short-lived JSON Web Token (JWT) for a browser session.
2.  **Client Authentication:** Authenticating a machine client (e.g., a script using a Vectron SDK) that is performing database operations. This is handled via long-lived, securely stored API keys.

The `auth` system provides the interface for users to generate and manage the API keys that are used in the second flow.

---

## Components

### 1. [Auth Service (`./service/`)](./service/README.md)

This is a backend gRPC service written in Go. It acts as the central authority for user and key management.

- **Responsibilities:**
    - Manages user registration and login.
    - Stores user credentials and API key data in `etcd`, using `bcrypt` to securely hash all sensitive information (passwords and full API keys).
    - Issues session JWTs for the frontend.
    - Provides an internal endpoint for the `apigateway` to validate client API keys.

[**Click here for the detailed Auth Service README.**](./service/README.md)

### 2. [Auth Frontend (`./frontend/`)](./frontend/README.md)

This is a single-page application (SPA) built with React and TypeScript. It provides the user interface for all account management tasks.

- **Responsibilities:**
    - Provides pages for users to sign up and log in.
    - Communicates with the Auth Service via its RESTful HTTP/JSON interface.
    - After login, it provides a protected dashboard where users can create, view, and revoke their API keys.

[**Click here for the detailed Auth Frontend README.**](./frontend/README.md)

## Interaction Diagram

```
+----------------+      1. Login      +----------------+      2. Manage API Keys      +-----------------+
|                | -----------------> |                | ---------------------------> |                 |
|      User      |                    |  Auth Frontend |                              |   Auth Service  |
| (via Browser)  | <----------------- |     (React)    | <--------------------------- |     (Go gRPC)   |
|                |   2. Receives UI   |                |  3. Receives JWT & API Keys  |                 |
+----------------+                    +----------------+                              +-----------------+
                                                                                             |
                                                                                             | 4. Validate API Key
                                                                                             |
                                                                                    +-----------------+
                                                                                    |                 |
                                                                                    |   API Gateway   |
                                                                                    |                 |
                                                                                    +-----------------+
```

1.  A user accesses the **Auth Frontend** in their browser to log in.
2.  The frontend communicates with the **Auth Service** to authenticate the user and retrieve a session JWT.
3.  Using the JWT, the user interacts with the frontend to manage their API keys. All management operations are sent to the Auth Service.
4.  When a separate client application uses a generated API key to access the Vectron database, the **API Gateway** calls the **Auth Service** to validate the key before processing the request.
