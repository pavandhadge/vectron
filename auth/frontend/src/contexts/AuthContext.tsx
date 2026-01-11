import React, { createContext, useState, useContext, useEffect, ReactNode } from 'react';
import axios from 'axios';
import { User, LoginRequest, RegisterUserRequest, LoginResponse, RegisterUserResponse } from '../api-types';

interface AuthContextType {
    user: User | null;
    token: string | null;
    isLoading: boolean;
    error: string | null;
    login: (data: LoginRequest) => Promise<void>;
    signup: (data: RegisterUserRequest) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

// Create an axios instance for API calls
const apiClient = axios.create({
    baseURL: import.meta.env.VITE_API_BASE_URL || '/api', // Use environment variable for API base URL
    headers: {
        'Content-Type': 'application/json',
    },
});

// Request interceptor for API calls
apiClient.interceptors.request.use(
    config => {
        const token = localStorage.getItem('jwt_token');
        if (token) {
            config.headers['Authorization'] = `Bearer ${token}`;
        }
        return config;
    },
    error => {
        return Promise.reject(error);
    }
);

export const AuthProvider = ({ children }: { ReactNode }) => {
    const [user, setUser] = useState<User | null>(null);
    const [token, setToken] = useState<string | null>(() => localStorage.getItem('jwt_token'));
    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        // The interceptor now handles setting the Authorization header.
        // We only need to fetch user profile if token exists and user is not set.
        if (token && !user) {
            apiClient.get('/v1/user/profile')
                .then(response => {
                    setUser(response.data.user);
                })
                .catch((err) => {
                    console.error("Error fetching user profile:", err);
                    setError("Failed to fetch user profile. Please log in again.");
                    logout(); // Token might be invalid or expired
                })
                .finally(() => setIsLoading(false));
        } else {
            setIsLoading(false);
        }
    }, [token, user]); // Added user to dependencies to re-run if user becomes null for some reason

    const login = async (data: LoginRequest) => {
        setError(null); // Clear previous errors
        try {
            const response = await apiClient.post<LoginResponse>('/v1/users/login', data);
            const { jwt_token, user } = response.data;
            localStorage.setItem('jwt_token', jwt_token);
            setToken(jwt_token);
            setUser(user);
        } catch (err: any) {
            console.error("Login failed:", err);
            setError(err.response?.data?.message || "Login failed. Please check your credentials.");
            throw err; // Re-throw to allow components to handle
        }
    };

    const signup = async (data: RegisterUserRequest) => {
        setError(null); // Clear previous errors
        try {
            await apiClient.post<RegisterUserResponse>('/v1/users/register', data);
            // After signup, automatically log in
            await login({ email: data.email, password: data.password });
        } catch (err: any) {
            console.error("Signup failed:", err);
            setError(err.response?.data?.message || "Signup failed. Please try again.");
            throw err; // Re-throw to allow components to handle
        }
    };

    const logout = () => {
        localStorage.removeItem('jwt_token');
        setToken(null);
        setUser(null);
        setError(null); // Clear any errors on logout
    };

    return (
        <AuthContext.Provider value={{ user, token, isLoading, error, login, signup, logout }}>
            {children}
        </AuthContext.Provider>
    );
};

export const useAuth = () => {
    const context = useContext(AuthContext);
    if (context === undefined) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return context;
};
