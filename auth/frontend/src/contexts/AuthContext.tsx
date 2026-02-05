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
  Plan,
  SubscriptionStatus,
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
   Normalization Helpers
======================= */

const normalizePlan = (plan: unknown): Plan => {
  if (typeof plan === "number") return plan as Plan;
  if (typeof plan === "string") {
    const mapped = (Plan as Record<string, unknown>)[plan];
    if (typeof mapped === "number") return mapped as Plan;
  }
  return Plan.FREE;
};

const normalizeSubscriptionStatus = (status: unknown): SubscriptionStatus => {
  if (typeof status === "number") return status as SubscriptionStatus;
  if (typeof status === "string") {
    const mapped = (SubscriptionStatus as Record<string, unknown>)[status];
    if (typeof mapped === "number") return mapped as SubscriptionStatus;
  }
  return SubscriptionStatus.SUBSCRIPTION_STATUS_UNSPECIFIED;
};

const normalizeUserProfile = (user: UserProfile): UserProfile => ({
  ...user,
  plan: normalizePlan(user.plan as unknown),
  subscription_status: normalizeSubscriptionStatus(
    user.subscription_status as unknown,
  ),
});

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
    setUser(normalizeUserProfile(user));
    localStorage.setItem("jwt_token", token);
  };

  useEffect(() => {
    if (token && !user) {
      authApiClient
        .get("/v1/user/profile")
        .then((response) => {
          setUser(normalizeUserProfile(response.data.user));
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
