/// <reference types="vite/client" />
import axios, {
  AxiosInstance,
  InternalAxiosRequestConfig,
} from "axios";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  ReactNode,
} from "react";
import {
  LoginRequest,
  LoginResponse,
  RegisterUserRequest,
  RegisterUserResponse,
  UserProfile,
} from "../api-types";

/* =======================
   Types
======================= */

interface AuthContextType {
  user: UserProfile | null;
  setUser: (user: UserProfile) => void;
  token: string | null;
  isLoading: boolean;
  error: string | null;
  login: (data: LoginRequest) => Promise<void>;
  signup: (data: RegisterUserRequest) => Promise<void>;
  logout: () => void;
  updateUserAndToken: (user: UserProfile, token: string) => void;
  authApiClient: AxiosInstance;
  apiGatewayApiClient: AxiosInstance;
}

/* =======================
   Context
======================= */

const AuthContext = createContext<AuthContextType | undefined>(undefined);

/* =======================
   Axios Instances
======================= */

const authApiClient = axios.create({
  baseURL:
    import.meta.env.VITE_AUTH_API_BASE_URL || "http://localhost:10009",
  headers: {
    "Content-Type": "application/json",
  },
});

const apiGatewayApiClient = axios.create({
  baseURL:
    import.meta.env.VITE_APIGATEWAY_API_BASE_URL || "http://localhost:10012",
  headers: {
    "Content-Type": "application/json",
  },
});

/* =======================
   Axios Interceptor
======================= */

const authInterceptor = (config: InternalAxiosRequestConfig) => {
  const token = localStorage.getItem("jwt_token");
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
};

authApiClient.interceptors.request.use(authInterceptor);
apiGatewayApiClient.interceptors.request.use(authInterceptor);

/* =======================
   Provider
======================= */

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<UserProfile | null>(null);
  const [token, setToken] = useState<string | null>(() =>
    localStorage.getItem("jwt_token"),
  );
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  const logout = () => {
    localStorage.removeItem("jwt_token");
    setToken(null);
    setUser(null);
    setError(null);
  };

  const updateUserAndToken = (user: UserProfile, token: string) => {
    setToken(token);
    setUser(user);
    localStorage.setItem("jwt_token", token);
  };

  useEffect(() => {
    if (token && !user) {
      authApiClient
        .get("/v1/user/profile")
        .then((response) => {
          setUser(response.data.user);
        })
        .catch((err) => {
          console.error("Error fetching user profile:", err);
          setError("Failed to fetch user profile. Please log in again.");
          logout();
        })
        .finally(() => setIsLoading(false));
    } else {
      setIsLoading(false);
    }
  }, [token, user]);

  const login = async (data: LoginRequest) => {
    setError(null);
    try {
      const response = await authApiClient.post<LoginResponse>(
        "/v1/users/login",
        data,
      );
      const { jwtToken, user } = response.data;
      updateUserAndToken(user, jwtToken);
    } catch (err: any) {
      console.error("Login failed:", err);
      setError(
        err.response?.data?.message ||
          "Login failed. Please check your credentials.",
      );
      throw err;
    }
  };

  const signup = async (data: RegisterUserRequest) => {
    setError(null);
    try {
      await authApiClient.post<RegisterUserResponse>(
        "/v1/users/register",
        data,
      );
      await login({ email: data.email, password: data.password });
    } catch (err: any) {
      console.error("Signup failed:", err);
      setError(
        err.response?.data?.message || "Signup failed. Please try again.",
      );
      throw err;
    }
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        setUser,
        token,
        isLoading,
        error,
        login,
        signup,
        logout,
        updateUserAndToken,
        authApiClient,
        apiGatewayApiClient,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

/* =======================
   Hook
======================= */

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
};