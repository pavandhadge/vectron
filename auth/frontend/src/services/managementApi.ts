import axios from 'axios';
import {
  WorkerNode,
  Collection,
  SystemHealth,
  GatewayStats,
  GatewayEndpoint,
  ListWorkersResponse,
  ListCollectionsResponse,
  GetSystemHealthResponse,
  GetGatewayStatsResponse,
} from '../api-types';

class ManagementApiService {
  private authBaseUrl = import.meta.env.VITE_AUTH_API_BASE_URL || 'http://localhost:10009';
  private apiGatewayUrl = import.meta.env.VITE_APIGATEWAY_API_BASE_URL || 'http://localhost:10012';
  private placementDriverUrl = import.meta.env.VITE_PLACEMENT_DRIVER_API_BASE_URL || 'http://localhost:10001';

  private getAuthHeaders() {
    const token = localStorage.getItem('jwtToken');
    return token ? { Authorization: `Bearer ${token}` } : {};
  }

  // ===== WORKER MANAGEMENT =====
  
  async getWorkers(): Promise<WorkerNode[]> {
    try {
      // For now, return mock data since backend doesn't expose REST management endpoints
      // TODO: Add management REST APIs to placement driver or create dedicated management service
      console.warn('Using mock worker data - management endpoints not yet implemented');
      return this.getMockWorkers();
    } catch (error) {
      console.warn('Failed to get workers from placement driver, using mock data');
      // Return mock data for development
      return this.getMockWorkers();
    }
  }

  async getWorkerStats(workerId: string): Promise<WorkerNode | null> {
    try {
      const workers = await this.getWorkers();
      return workers.find(w => w.worker_id === workerId) || null;
    } catch (error) {
      console.error('Failed to get worker stats:', error);
      return null;
    }
  }

  // ===== COLLECTION MANAGEMENT =====
  
  async getCollections(): Promise<Collection[]> {
    try {
      // Try to get collections from API Gateway
      const response = await axios.get<ListCollectionsResponse>(
        `${this.apiGatewayUrl}/v1/collections`,
        { headers: this.getAuthHeaders() }
      );
      return response.data.collections;
    } catch (error) {
      console.warn('Failed to get collections, using mock data');
      return this.getMockCollections();
    }
  }

  async createCollection(name: string, dimension: number, distance: string): Promise<void> {
    try {
      await axios.post(
        `${this.apiGatewayUrl}/v1/collections`,
        { name, dimension, distance },
        { headers: this.getAuthHeaders() }
      );
    } catch (error) {
      console.error('Failed to create collection:', error);
      throw error;
    }
  }

  async deleteCollection(name: string): Promise<void> {
    try {
      await axios.delete(
        `${this.apiGatewayUrl}/v1/collections/${name}`,
        { headers: this.getAuthHeaders() }
      );
    } catch (error) {
      console.error('Failed to delete collection:', error);
      throw error;
    }
  }

  // ===== SYSTEM HEALTH =====
  
  async getSystemHealth(): Promise<SystemHealth> {
    try {
      // For now, return mock data since backend doesn't have these endpoints yet
      // TODO: Implement actual management endpoints in backend services
      console.warn('Using mock system health data - management endpoints not yet implemented');
      return this.getMockSystemHealth();
    } catch (error) {
      console.warn('Failed to get system health, using mock data');
      return this.getMockSystemHealth();
    }
  }

  // ===== API GATEWAY MANAGEMENT =====
  
  async getGatewayStats(): Promise<{ stats: GatewayStats; endpoints: GatewayEndpoint[] }> {
    try {
      // For now, return mock data since API Gateway doesn't have admin stats endpoint yet
      // TODO: Implement /v1/admin/stats endpoint in API Gateway
      console.warn('Using mock gateway stats - admin endpoints not yet implemented');
      return this.getMockGatewayStats();
    } catch (error) {
      console.warn('Failed to get gateway stats, using mock data');
      return this.getMockGatewayStats();
    }
  }

  // ===== MOCK DATA FOR DEVELOPMENT =====
  
  private getMockWorkers(): WorkerNode[] {
    return [
      {
        worker_id: 'worker-1',
        grpc_address: '127.0.0.1:10007',
        raft_address: '127.0.0.1:10008',
        collections: ['embeddings', 'images'],
        last_heartbeat: Date.now() - 5000,
        healthy: true,
        metadata: { gpu: 'true', memory: '32GB', region: 'us-east-1' },
        stats: {
          vector_count: 125000,
          memory_bytes: 2147483648,
          cpu_usage: 45,
          disk_usage: 1073741824,
          uptime: 86400,
          shard_info: [
            {
              shard_id: 1,
              collection: 'embeddings',
              is_leader: true,
              replica_count: 2,
              vector_count: 75000,
              size_bytes: 1073741824,
              status: 'healthy'
            }
          ]
        }
      },
      {
        worker_id: 'worker-2',
        grpc_address: '127.0.0.1:10008',
        raft_address: '127.0.0.1:10009',
        collections: ['embeddings', 'documents'],
        last_heartbeat: Date.now() - 3000,
        healthy: true,
        metadata: { gpu: 'false', memory: '16GB', region: 'us-west-2' },
        stats: {
          vector_count: 98000,
          memory_bytes: 1610612736,
          cpu_usage: 32,
          disk_usage: 805306368,
          uptime: 72000,
          shard_info: []
        }
      }
    ];
  }

  private getMockCollections(): Collection[] {
    return [
      {
        name: 'embeddings',
        dimension: 1536,
        distance: 'cosine',
        created_at: Date.now() - 86400000,
        vector_count: 175000,
        size_bytes: 1879048192,
        shard_count: 4,
        status: 'active',
        shards: [
          {
            shard_id: 1,
            worker_ids: ['worker-1', 'worker-2'],
            leader_id: 'worker-1',
            vector_count: 75000,
            size_bytes: 805306368,
            status: 'healthy',
            last_updated: Date.now() - 60000
          }
        ]
      },
      {
        name: 'images',
        dimension: 512,
        distance: 'euclidean',
        created_at: Date.now() - 43200000,
        vector_count: 89000,
        size_bytes: 456704000,
        shard_count: 2,
        status: 'active',
        shards: []
      }
    ];
  }

  private getMockSystemHealth(): SystemHealth {
    return {
      overall_status: 'healthy',
      services: [
        {
          name: 'API Gateway',
          status: 'up',
          endpoint: 'http://localhost:10012',
          last_check: Date.now() - 1000,
          response_time: 12
        },
        {
          name: 'Auth Service',
          status: 'up',
          endpoint: 'http://localhost:10009',
          last_check: Date.now() - 1000,
          response_time: 8
        },
        {
          name: 'Placement Driver',
          status: 'up',
          endpoint: 'http://localhost:10001',
          last_check: Date.now() - 1000,
          response_time: 15
        },
        {
          name: 'Worker Node 1',
          status: 'up',
          endpoint: 'http://localhost:10007',
          last_check: Date.now() - 2000,
          response_time: 18
        }
      ],
      alerts: [
        {
          id: 'alert-1',
          level: 'warning',
          title: 'High Memory Usage',
          message: 'Worker-1 memory usage is at 85%',
          timestamp: Date.now() - 300000,
          resolved: false,
          source: 'worker-1'
        }
      ],
      metrics: {
        total_vectors: 264000,
        total_collections: 3,
        active_workers: 2,
        storage_used: 2335752192,
        memory_used: 3758096384,
        cpu_usage: 38,
        network_io: {
          bytes_in: 1048576000,
          bytes_out: 2097152000
        }
      }
    };
  }

  private getMockGatewayStats(): { stats: GatewayStats; endpoints: GatewayEndpoint[] } {
    return {
      stats: {
        uptime: 86400,
        totalRequests: 125000,
        requestsPerSecond: 45,
        activeConnections: 23,
        errorRate: 0.02,
        averageResponseTime: 125,
        rateLimit: {
          enabled: true,
          rps: 100,
          activeUsers: 12
        }
      },
      endpoints: [
        {
          path: '/v1/collections/{collection}/points/search',
          method: 'POST',
          count: 45000,
          avgLatency: 180,
          errorCount: 23
        },
        {
          path: '/v1/collections/{collection}/points',
          method: 'POST',
          count: 32000,
          avgLatency: 95,
          errorCount: 12
        },
        {
          path: '/v1/collections',
          method: 'GET',
          count: 8500,
          avgLatency: 35,
          errorCount: 2
        }
      ]
    };
  }
}

export const managementApi = new ManagementApiService();