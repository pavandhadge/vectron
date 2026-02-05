import axios from 'axios';
import {
  WorkerNode,
  Collection,
  SystemHealth,
  GatewayStats,
  GatewayEndpoint,
  Alert,
  ListWorkersResponse,
  ListCollectionsResponse,
  GetGatewayStatsResponse,
} from '../api-types';

class ManagementApiService {
  private apiGatewayUrl =
    import.meta.env.VITE_APIGATEWAY_API_BASE_URL || 'http://localhost:10012';

  private getAuthHeaders() {
    const token = localStorage.getItem('jwtToken');
    return token ? { Authorization: `Bearer ${token}` } : {};
  }

  // ===== WORKER MANAGEMENT =====
  
  async getWorkers(): Promise<WorkerNode[]> {
    try {
      const response = await axios.get<ListWorkersResponse>(
        `${this.apiGatewayUrl}/v1/admin/workers`,
        { headers: this.getAuthHeaders() }
      );
      return response.data.workers || [];
    } catch (error) {
      console.error('Failed to get workers:', error);
      throw error;
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
      const response = await axios.get<ListCollectionsResponse>(
        `${this.apiGatewayUrl}/v1/admin/collections`,
        { headers: this.getAuthHeaders() }
      );
      return response.data.collections || [];
    } catch (error) {
      console.error('Failed to get collections from admin endpoint, falling back to public API:', error);
      // Fallback to public API
      try {
        const response = await axios.get<ListCollectionsResponse>(
          `${this.apiGatewayUrl}/v1/collections`,
          { headers: this.getAuthHeaders() }
        );
        return response.data.collections || [];
      } catch (fallbackError) {
        console.error('Fallback also failed:', fallbackError);
        throw fallbackError;
      }
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
      const response = await axios.get<SystemHealth>(
        `${this.apiGatewayUrl}/v1/system/health`,
        { headers: this.getAuthHeaders() }
      );
      return response.data;
    } catch (error) {
      console.error('Failed to get system health:', error);
      throw error;
    }
  }

  // ===== API GATEWAY MANAGEMENT =====
  
  async getGatewayStats(): Promise<{ stats: GatewayStats; endpoints: GatewayEndpoint[] }> {
    try {
      const response = await axios.get<GetGatewayStatsResponse>(
        `${this.apiGatewayUrl}/v1/admin/stats`,
        { headers: this.getAuthHeaders() }
      );
      return {
        stats: response.data.stats,
        endpoints: response.data.endpoints || []
      };
    } catch (error) {
      console.error('Failed to get gateway stats:', error);
      throw error;
    }
  }

  // ===== ALERTS =====
  
  async getAlerts(status?: 'active' | 'resolved', level?: string): Promise<Alert[]> {
    try {
      const params = new URLSearchParams();
      if (status) params.append('status', status);
      if (level) params.append('level', level);
      
      const response = await axios.get<{alerts: Alert[], count: number}>(
        `${this.apiGatewayUrl}/v1/alerts?${params.toString()}`,
        { headers: this.getAuthHeaders() }
      );
      return response.data.alerts || [];
    } catch (error) {
      console.error('Failed to get alerts:', error);
      throw error;
    }
  }

  async resolveAlert(alertId: string): Promise<void> {
    try {
      await axios.post(
        `${this.apiGatewayUrl}/v1/alerts/${alertId}/resolve`,
        {},
        { headers: this.getAuthHeaders() }
      );
    } catch (error) {
      console.error('Failed to resolve alert:', error);
      throw error;
    }
  }
}

export const managementApi = new ManagementApiService();
