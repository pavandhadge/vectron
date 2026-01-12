// src/api-types.ts
// This file defines the TypeScript types for our API communication.

export enum Plan {
  PLAN_UNSPECIFIED = 0,
  FREE = 1,
  PAID = 2,
}

export enum SubscriptionStatus {
  SUBSCRIPTION_STATUS_UNSPECIFIED = 0,
  ACTIVE = 1,
  CANCELED = 2,
  PAST_DUE = 3,
}

export interface UserProfile {
  id: string;
  email: string;
  created_at: number;
  plan: Plan;
  subscription_status: SubscriptionStatus;
}
export interface ApiKey {
  keyPrefix: string;
  userId: string;
  createdAt: number;
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
  user: UserProfile;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  jwtToken: string;
  user: UserProfile;
}

export interface CreateSDKJWTRequest {
  api_key_id: string;
}

export interface CreateSDKJWTResponse {
  sdkjwt: string;
}

export interface UpdateUserProfileResponse {
  user: UserProfile;
  jwtToken: string;
}
