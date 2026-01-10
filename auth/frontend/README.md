# Vectron Auth Frontend

This directory contains the source code for the Vectron authentication and account management web interface. It is a modern single-page application (SPA) built with React and TypeScript.

## Features

- **User Registration:** Allows new users to create an account.
- **User Login/Logout:** Provides a secure login form and session management.
- **Dashboard:** A protected area for logged-in users.
- **API Key Management:** The primary feature of the dashboard, allowing users to create, view, and delete their API keys for accessing the Vectron database.

## Tech Stack

- **Framework:** [React](https://react.dev/)
- **Language:** [TypeScript](https://www.typescriptlang.org/)
- **Build Tool:** [Vite](https://vitejs.dev/)
- **Routing:** [React Router](https://reactrouter.com/)
- **API Communication:** [Axios](https://axios-http.com/)

## Project Structure

The `src` directory is organized into the following key areas:

- `main.tsx`: The main entry point of the application.
- `App.tsx`: The root component that sets up the global context providers and defines the application's routes.
- `pages/`: Components that represent full pages in the application (e.g., `LoginPage`, `Dashboard`).
- `components/`: Reusable, smaller React components used across various pages (e.g., `Navbar`, `Layout`, `ProtectedRoute`).
- `contexts/`: Contains React Context providers for global state management.
- `api-types.ts`: Holds TypeScript type definitions for objects used in API communication, ensuring type safety.

## Architecture and Design

### 1. State Management (`AuthContext`)

The application's global authentication state is managed within `src/contexts/AuthContext.tsx`.

- **Provider:** An `AuthProvider` component wraps the entire application.
- **State:** It manages the user's authentication status, including the current `user` object and the session `token` (JWT).
- **Persistence:** Upon successful login, the JWT is stored in the browser's `localStorage`. When the application loads, it checks `localStorage` for an existing token to re-establish the session.
- **Session Validation:** If a token is found in `localStorage`, the context sends a request to the backend's `/v1/user/profile` endpoint to validate the token and fetch the user's data. If the token is invalid or expired, the session is cleared.
- **`useAuth` Hook:** A custom hook, `useAuth()`, is provided for components to easily access the authentication state and functions like `login`, `logout`, and `signup`.

### 2. Routing and Protected Routes

The application uses `react-router-dom` to manage client-side navigation.

- **Public Routes:** Routes for the home page, login, and signup are publicly accessible.
- **Protected Routes:** The `/dashboard` area is protected using a custom `ProtectedRoute` component.
  - This component uses the `useAuth` hook to check if a user is authenticated.
  - If the user is not logged in, it redirects them to the `/login` page.
  - It preserves the user's originally intended destination, redirecting them back to that page after a successful login.

### 3. API Communication

- An `axios` instance is used for all HTTP communication with the `auth-service` backend.
- The `AuthContext` is responsible for setting the `Authorization: Bearer <token>` header on the global `axios` instance after login, so all subsequent requests are automatically authenticated.

## Local Development

1.  **Install dependencies:**
    ```bash
    npm install
    ```
2.  **Run the development server:**
    ```bash
    npm run dev
    ```
    This will start the Vite development server, typically on `http://localhost:5173`. The application expects the `auth-service` to be running and accessible. By default, Vite is configured to proxy requests from `/v1` to the auth service's backend at `http://localhost:8082`.

---

This README provides a detailed overview of the `auth/frontend`. The next step is to create a top-level README for the entire `auth` directory.
