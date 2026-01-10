// src/api-types.ts
// This file defines the TypeScript types for our API communication.

export interface User {
  id: string;
  email: string;
  created_at: number;
}

export interface ApiKey {
  key_prefix: string;
  user_id: string;
  created_at: number;
  name: string;
}

export interface ListKeysResponse {
  keys: ApiKey[];
}

export interface CreateKeyRequest {
  name: string;
}

export interface CreateKeyResponse {
  full_key: string;
  key_info: ApiKey;
}

export interface DeleteKeyRequest {
  key_prefix: string;
}

export interface RegisterUserRequest {
  email: string;
  password: string;
}

export interface RegisterUserResponse {
  user: User;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  jwt_token: string;
  user: User;
}
