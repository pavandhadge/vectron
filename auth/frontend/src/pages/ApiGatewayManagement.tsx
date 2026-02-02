import React, { useState, useEffect } from "react";
import {
  Activity,
  BarChart3,
  Clock,
  RefreshCw,
  Users,
  Zap,
  Globe,
  Eye,
  EyeOff,
  TrendingUp,
  AlertCircle,
} from "lucide-react";
import { GatewayStats, GatewayEndpoint } from "../api-types";
import { managementApi } from "../services/managementApi";

interface ApiGatewayManagementProps {}

const ApiGatewayManagement: React.FC<ApiGatewayManagementProps> = () => {
  const [gatewayStats, setGatewayStats] = useState<GatewayStats | null>(null);
  const [endpoints, setEndpoints] = useState<GatewayEndpoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const fetchGatewayStats = async () => {
    try {
      setError(null);
      const data = await managementApi.getGatewayStats();
      setGatewayStats(data.stats);
      setEndpoints(data.endpoints);
    } catch (err) {
      setError("Failed to fetch API Gateway statistics");
      console.error("Error fetching gateway stats:", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchGatewayStats();
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;
    
    const interval = setInterval(() => {
      fetchGatewayStats();
    }, 30000); // Refresh every 30 seconds

    return () => clearInterval(interval);
  }, [autoRefresh]);

  const formatUptime = (seconds: number) => {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  };

  const formatNumber = (num: number) => {
    if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
    if (num >= 1000) return `${(num / 1000).toFixed(1)}K`;
    return num.toString();
  };

  const getErrorRateColor = (rate: number) => {
    if (rate < 0.01) return "text-green-400";
    if (rate < 0.05) return "text-yellow-400";
    return "text-red-400";
  };

  const getLatencyColor = (latency: number) => {
    if (latency < 100) return "text-green-400";
    if (latency < 500) return "text-yellow-400";
    return "text-red-400";
  };

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="flex items-center justify-center py-12">
          <RefreshCw className="w-8 h-8 text-purple-400 animate-spin" />
        </div>
      </div>
    );
  }

  if (error || !gatewayStats) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="text-center py-12">
          <AlertCircle className="w-12 h-12 text-red-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-white mb-2">Failed to Load Gateway Statistics</h2>
          <p className="text-neutral-400 mb-4">{error}</p>
          <button
            onClick={fetchGatewayStats}
            className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors"
          >
            Try Again
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-white mb-2">
            API Gateway Management
          </h1>
          <p className="text-neutral-400">
            Monitor API Gateway performance, rate limiting, and endpoint statistics.
          </p>
        </div>
        
        <div className="flex items-center gap-3">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-colors ${
              autoRefresh
                ? "bg-purple-600 text-white hover:bg-purple-700"
                : "bg-neutral-800 text-neutral-300 hover:bg-neutral-700"
            }`}
          >
            {autoRefresh ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
            Auto Refresh
          </button>
          
          <button
            onClick={fetchGatewayStats}
            className="flex items-center gap-2 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh
          </button>
        </div>
      </div>

      {/* Performance Metrics */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Clock className="w-5 h-5 text-green-400" />
            <h3 className="font-medium text-white">Uptime</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatUptime(gatewayStats.uptime)}
          </div>
          <p className="text-sm text-neutral-400">System Uptime</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <BarChart3 className="w-5 h-5 text-blue-400" />
            <h3 className="font-medium text-white">Total Requests</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatNumber(gatewayStats.totalRequests)}
          </div>
          <p className="text-sm text-neutral-400">Total Processed</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <TrendingUp className="w-5 h-5 text-purple-400" />
            <h3 className="font-medium text-white">Requests/sec</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {gatewayStats.requestsPerSecond}
          </div>
          <p className="text-sm text-neutral-400">Current Rate</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <AlertCircle className="w-5 h-5 text-red-400" />
            <h3 className="font-medium text-white">Error Rate</h3>
          </div>
          <div className={`text-2xl font-bold ${getErrorRateColor(gatewayStats.errorRate)}`}>
            {(gatewayStats.errorRate * 100).toFixed(2)}%
          </div>
          <p className="text-sm text-neutral-400">Current Error Rate</p>
        </div>
      </div>

      {/* Network & Performance Stats */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Connection Stats */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6">
          <h2 className="text-xl font-semibold text-white mb-6">Network Statistics</h2>
          
          <div className="space-y-4">
            <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-800/50">
              <div className="flex items-center gap-3">
                <Users className="w-5 h-5 text-blue-400" />
                <span className="text-white">Active Connections</span>
              </div>
              <div className="text-xl font-bold text-white">
                {gatewayStats.activeConnections}
              </div>
            </div>

            <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-800/50">
              <div className="flex items-center gap-3">
                <Activity className="w-5 h-5 text-green-400" />
                <span className="text-white">Average Response Time</span>
              </div>
              <div className={`text-xl font-bold ${getLatencyColor(gatewayStats.averageResponseTime)}`}>
                {gatewayStats.averageResponseTime}ms
              </div>
            </div>
          </div>
        </div>

        {/* Rate Limiting Status */}
        <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6">
          <h2 className="text-xl font-semibold text-white mb-6">Rate Limiting</h2>
          
          <div className="space-y-4">
            <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-800/50">
              <div className="flex items-center gap-3">
                <div className={`w-3 h-3 rounded-full ${gatewayStats.rateLimit.enabled ? "bg-green-400" : "bg-red-400"}`} />
                <span className="text-white">Status</span>
              </div>
              <div className={`font-semibold ${gatewayStats.rateLimit.enabled ? "text-green-400" : "text-red-400"}`}>
                {gatewayStats.rateLimit.enabled ? "ENABLED" : "DISABLED"}
              </div>
            </div>

            {gatewayStats.rateLimit.enabled && (
              <>
                <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-800/50">
                  <div className="flex items-center gap-3">
                    <Zap className="w-5 h-5 text-yellow-400" />
                    <span className="text-white">Rate Limit</span>
                  </div>
                  <div className="text-xl font-bold text-white">
                    {gatewayStats.rateLimit.rps} RPS
                  </div>
                </div>

                <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-800/50">
                  <div className="flex items-center gap-3">
                    <Users className="w-5 h-5 text-purple-400" />
                    <span className="text-white">Active Users</span>
                  </div>
                  <div className="text-xl font-bold text-white">
                    {gatewayStats.rateLimit.activeUsers}
                  </div>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Endpoint Statistics */}
      <div className="rounded-xl border border-neutral-800 bg-neutral-900/50">
        <div className="p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Endpoint Statistics</h2>
        </div>
        
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-neutral-800/50">
              <tr>
                <th className="text-left p-4 text-neutral-400 font-medium">Method</th>
                <th className="text-left p-4 text-neutral-400 font-medium">Path</th>
                <th className="text-right p-4 text-neutral-400 font-medium">Requests</th>
                <th className="text-right p-4 text-neutral-400 font-medium">Avg Latency</th>
                <th className="text-right p-4 text-neutral-400 font-medium">Errors</th>
                <th className="text-right p-4 text-neutral-400 font-medium">Error Rate</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {endpoints.map((endpoint, index) => {
                const errorRate = endpoint.count > 0 ? endpoint.errorCount / endpoint.count : 0;
                
                return (
                  <tr key={index} className="hover:bg-neutral-800/30 transition-colors">
                    <td className="p-4">
                      <span className={`px-2 py-1 rounded text-xs font-medium ${
                        endpoint.method === "GET" ? "bg-green-500/10 text-green-400" :
                        endpoint.method === "POST" ? "bg-blue-500/10 text-blue-400" :
                        endpoint.method === "PUT" ? "bg-yellow-500/10 text-yellow-400" :
                        endpoint.method === "DELETE" ? "bg-red-500/10 text-red-400" :
                        "bg-neutral-500/10 text-neutral-400"
                      }`}>
                        {endpoint.method}
                      </span>
                    </td>
                    <td className="p-4">
                      <span className="text-white font-mono text-sm">
                        {endpoint.path}
                      </span>
                    </td>
                    <td className="p-4 text-right text-white font-medium">
                      {formatNumber(endpoint.count)}
                    </td>
                    <td className="p-4 text-right">
                      <span className={`font-medium ${getLatencyColor(endpoint.avgLatency)}`}>
                        {endpoint.avgLatency}ms
                      </span>
                    </td>
                    <td className="p-4 text-right text-white font-medium">
                      {endpoint.errorCount}
                    </td>
                    <td className="p-4 text-right">
                      <span className={`font-medium ${getErrorRateColor(errorRate)}`}>
                        {(errorRate * 100).toFixed(2)}%
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          
          {endpoints.length === 0 && (
            <div className="p-8 text-center">
              <Globe className="w-12 h-12 text-neutral-500 mx-auto mb-4" />
              <h3 className="text-lg font-medium text-white mb-2">No Endpoints Found</h3>
              <p className="text-neutral-400">No endpoint statistics available</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export { ApiGatewayManagement };
export default ApiGatewayManagement;