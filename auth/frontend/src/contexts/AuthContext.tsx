import axios, { AxiosInstance } from "axios";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  ReactNode,
} from "react";

/* =======================
   Types
======================= */

interface AuthContextType {
  user: User | null;
  token: string | null;
  isLoading: boolean;
  error: string | null;
  login: (data: LoginRequest) => Promise<void>;
  signup: (data: RegisterUserRequest) => Promise<void>;
  logout: () => void;
  apiClient: AxiosInstance;
}

/* =======================
   Context
======================= */

const AuthContext = createContext<AuthContextType | undefined>(undefined);

/* =======================
   Axios Instance
======================= */

const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || "http://localhost:8082",
  headers: {
    "Content-Type": "application/json",
  },
});

/* =======================
   Axios Interceptor
======================= */

apiClient.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem("jwt_token");

    if (token) {
      console.log(token);
      config.headers = config.headers ?? {};
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error),
);

/* =======================
   Provider
======================= */

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  const [user, setUser] = useState<User | null>(null);
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

  useEffect(() => {
    if (token && !user) {
      apiClient
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
      const response = await apiClient.post<LoginResponse>(
        "/v1/users/login",
        data,
      );
      // FIX: Changed jwt_token to jwtToken to match backend response
      const { jwtToken, user } = response.data;

      localStorage.setItem("jwt_token", jwtToken); // Use jwtToken here
      setToken(jwtToken); // Use jwtToken here
      setUser(user);
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
      await apiClient.post<RegisterUserResponse>("/v1/users/register", data);
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
        token,
        isLoading,
        error,
        login,
        signup,
        logout,
        apiClient,
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
