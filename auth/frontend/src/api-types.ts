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
  sdkJwt: string;
}

export interface UpdateUserProfileResponse {
  user: UserProfile;
  jwtToken: string;
}

// ===== MANAGEMENT CONSOLE TYPES =====

// API Gateway Types
export interface GatewayStats {
  uptime: number;
  totalRequests: number;
  requestsPerSecond: number;
  activeConnections: number;
  errorRate: number;
  averageResponseTime: number;
  rateLimit: {
    enabled: boolean;
    rps: number;
    activeUsers: number;
  };
}

export interface GatewayEndpoint {
  path: string;
  method: string;
  count: number;
  avgLatency: number;
  errorCount: number;
}

// Worker Types
export interface WorkerNode {
  worker_id: string;
  grpc_address: string;
  raft_address: string;
  collections: string[];
  last_heartbeat: number;
  healthy: boolean;
  metadata: Record<string, string>;
  stats?: WorkerStats;
}

export interface WorkerStats {
  vector_count: number;
  memory_bytes: number;
  cpu_usage: number;
  disk_usage: number;
  uptime: number;
  shard_info: ShardInfo[];
}

export interface ShardInfo {
  shard_id: number;
  collection: string;
  is_leader: boolean;
  replica_count: number;
  vector_count: number;
  size_bytes: number;
  status: 'healthy' | 'degraded' | 'error';
}

// Collection Types
export interface Collection {
  name: string;
  dimension: number;
  distance: string;
  created_at: number;
  vector_count: number;
  size_bytes: number;
  shard_count: number;
  status: 'active' | 'creating' | 'error';
  shards: CollectionShard[];
}

export interface CollectionShard {
  shard_id: number;
  worker_ids: string[];
  leader_id: string;
  vector_count: number;
  size_bytes: number;
  status: 'healthy' | 'degraded' | 'error';
  last_updated: number;
}

// System Health Types
export interface SystemHealth {
  overall_status: 'healthy' | 'degraded' | 'critical';
  services: ServiceStatus[];
  alerts: Alert[];
  metrics: SystemMetrics;
}

export interface ServiceStatus {
  name: string;
  status: 'up' | 'down' | 'degraded';
  endpoint: string;
  last_check: number;
  response_time: number;
  error?: string;
}

export interface Alert {
  id: string;
  level: 'info' | 'warning' | 'error' | 'critical';
  title: string;
  message: string;
  timestamp: number;
  resolved: boolean;
  source: string;
}

export interface SystemMetrics {
  total_vectors: number;
  total_collections: number;
  active_workers: number;
  storage_used: number;
  memory_used: number;
  cpu_usage: number;
  network_io: {
    bytes_in: number;
    bytes_out: number;
  };
}

// Request/Response Types
export interface ListWorkersResponse {
  workers: WorkerNode[];
}

export interface ListCollectionsResponse {
  collections: Collection[];
}

export interface GetSystemHealthResponse {
  health: SystemHealth;
}

export interface GetGatewayStatsResponse {
  stats: GatewayStats;
  endpoints: GatewayEndpoint[];
}
