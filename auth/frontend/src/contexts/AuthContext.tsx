import React, { createContext, useState, useContext, useEffect, ReactNode } from 'react';
import axios from 'axios';
import { User, LoginRequest, RegisterUserRequest, LoginResponse, RegisterUserResponse } from '../api-types';

interface AuthContextType {
    user: User | null;
    token: string | null;
    isLoading: boolean;
    login: (data: LoginRequest) => Promise<void>;
    signup: (data: RegisterUserRequest) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

// Create an axios instance for API calls
const apiClient = axios.create();

export const AuthProvider = ({ children }: { children: ReactNode }) => {
    const [user, setUser] = useState<User | null>(null);
    const [token, setToken] = useState<string | null>(() => localStorage.getItem('jwt_token'));
    const [isLoading, setIsLoading] = useState<boolean>(true);

    useEffect(() => {
        if (token) {
            apiClient.defaults.headers.common['Authorization'] = `Bearer ${token}`;
            // Fetch user profile if we have a token but no user object
            if (!user) {
                axios.get('/v1/user/profile')
                    .then(response => {
                        setUser(response.data.user);
                    })
                    .catch(() => {
                        // Token is invalid, log out
                        logout();
                    })
                    .finally(() => setIsLoading(false));
            } else {
                 setIsLoading(false);
            }
        } else {
            setIsLoading(false);
        }
    }, [token]);

    const login = async (data: LoginRequest) => {
        const response = await axios.post<LoginResponse>('/v1/users/login', data);
        const { jwt_token, user } = response.data;
        localStorage.setItem('jwt_token', jwt_token);
        setToken(jwt_token);
        setUser(user);
    };

    const signup = async (data: RegisterUserRequest) => {
        await axios.post<RegisterUserResponse>('/v1/users/register', data);
        // After signup, automatically log in
        await login({ email: data.email, password: data.password });
    };

    const logout = () => {
        localStorage.removeItem('jwt_token');
        setToken(null);
        setUser(null);
        delete apiClient.defaults.headers.common['Authorization'];
    };

    return (
        <AuthContext.Provider value={{ user, token, isLoading, login, signup, logout }}>
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
