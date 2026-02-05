import React, { useState, useEffect } from "react";
import {
  CheckCircle,
  Database,
  MemoryStick,
  RefreshCw,
  Server,
  XCircle,
  Eye,
  EyeOff,
  AlertCircle,
  X,
} from "lucide-react";
import { WorkerNode } from "../api-types";
import { managementApi } from "../services/managementApi";
import {
  formatBytes,
  formatDateTime,
  formatNumber,
  formatUptime,
  toNumber,
} from "../utils/format";

interface WorkersManagementProps {}

interface WorkerModalProps {
  worker: WorkerNode;
  onClose: () => void;
}

const getHealthState = (worker: WorkerNode) => {
  const hasHeartbeat = toNumber(worker.last_heartbeat, 0) > 0;
  if (!hasHeartbeat) return "starting";
  return worker.healthy ? "healthy" : "unhealthy";
};

const WorkerModal: React.FC<WorkerModalProps> = ({ worker, onClose }) => {
  const formatCpuUsage = (value: unknown) =>
    formatNumber(toNumber(value, NaN), "--");
  const workerCollections = worker.collections ?? [];
  const workerMetadataEntries = worker.metadata ? Object.entries(worker.metadata) : [];
  const healthState = getHealthState(worker);
  const healthColor =
    healthState === "healthy"
      ? "text-green-400"
      : healthState === "starting"
      ? "text-yellow-400"
      : "text-red-400";

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-neutral-900 border border-neutral-800 rounded-xl max-w-4xl w-full max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Worker Details: {worker.worker_id}</h2>
          <button
            onClick={onClose}
            className="p-2 hover:bg-neutral-800 rounded-lg transition-colors"
          >
            <X className="w-5 h-5 text-neutral-400" />
          </button>
        </div>

        <div className="p-6 space-y-6">
          {/* Basic Info */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="space-y-4">
              <h3 className="text-lg font-semibold text-white">Connection Info</h3>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-neutral-400">Worker ID:</span>
                  <span className="text-white font-mono">{worker.worker_id}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">gRPC Address:</span>
                  <span className="text-white font-mono">{worker.grpc_address}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Raft Address:</span>
                  <span className="text-white font-mono">{worker.raft_address}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Status:</span>
                  <div className="flex items-center gap-2">
                    {healthState === "healthy" ? (
                      <CheckCircle className="w-4 h-4 text-green-400" />
                    ) : healthState === "starting" ? (
                      <AlertCircle className="w-4 h-4 text-yellow-400" />
                    ) : (
                      <XCircle className="w-4 h-4 text-red-400" />
                    )}
                    <span className={healthColor}>
                      {healthState === "healthy"
                        ? "Healthy"
                        : healthState === "starting"
                        ? "Starting"
                        : "Unhealthy"}
                    </span>
                  </div>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Last Heartbeat:</span>
                  <span className="text-white">
                    {formatDateTime(worker.last_heartbeat)}
                  </span>
                </div>
              </div>
            </div>

            {worker.stats && (
              <div className="space-y-4">
                <h3 className="text-lg font-semibold text-white">Performance Stats</h3>
                <div className="space-y-3">
                  <div className="flex justify-between">
                    <span className="text-neutral-400">Vector Count:</span>
                    <span className="text-white font-semibold">
                      {formatNumber(worker.stats.vector_count)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-neutral-400">Memory Usage:</span>
                    <span className="text-white font-semibold">
                      {formatBytes(worker.stats.memory_bytes)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-neutral-400">CPU Usage:</span>
                    <span className={`font-semibold ${
                      worker.stats.cpu_usage > 80 ? "text-red-400" :
                      worker.stats.cpu_usage > 60 ? "text-yellow-400" : "text-green-400"
                    }`}>
                      {formatCpuUsage(worker.stats.cpu_usage)}%
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-neutral-400">Disk Usage:</span>
                    <span className="text-white font-semibold">
                      {formatBytes(worker.stats.disk_usage)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-neutral-400">Uptime:</span>
                    <span className="text-white font-semibold">
                      {formatUptime(worker.stats.uptime)}
                    </span>
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Collections */}
          <div className="space-y-4">
            <h3 className="text-lg font-semibold text-white">Collections</h3>
            <div className="flex flex-wrap gap-2">
              {workerCollections.map((collection) => (
                <span
                  key={collection}
                  className="px-3 py-1 bg-purple-500/10 border border-purple-500/20 text-purple-400 rounded-lg text-sm"
                >
                  {collection}
                </span>
              ))}
            </div>
            {workerCollections.length === 0 && (
              <p className="text-neutral-400">No collections assigned</p>
            )}
          </div>

          {/* Metadata */}
          <div className="space-y-4">
            <h3 className="text-lg font-semibold text-white">Metadata</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {workerMetadataEntries.map(([key, value]) => (
                <div key={key} className="flex justify-between p-3 bg-neutral-800/50 rounded-lg">
                  <span className="text-neutral-400 capitalize">{key}:</span>
                  <span className="text-white font-mono">{value}</span>
                </div>
              ))}
            </div>
            {workerMetadataEntries.length === 0 && (
              <p className="text-neutral-400">No metadata available</p>
            )}
          </div>

          {/* Shard Info */}
          {worker.stats?.shard_info && worker.stats.shard_info.length > 0 && (
            <div className="space-y-4">
              <h3 className="text-lg font-semibold text-white">Shard Information</h3>
              <div className="overflow-x-auto">
                <table className="w-full border border-neutral-800 rounded-lg">
                  <thead className="bg-neutral-800/50">
                    <tr>
                      <th className="text-left p-3 text-neutral-400 font-medium">Shard ID</th>
                      <th className="text-left p-3 text-neutral-400 font-medium">Collection</th>
                      <th className="text-center p-3 text-neutral-400 font-medium">Leader</th>
                      <th className="text-right p-3 text-neutral-400 font-medium">Vectors</th>
                      <th className="text-right p-3 text-neutral-400 font-medium">Size</th>
                      <th className="text-center p-3 text-neutral-400 font-medium">Status</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-neutral-800">
                    {worker.stats.shard_info.map((shard) => (
                      <tr key={shard.shard_id}>
                        <td className="p-3 text-white font-mono">{shard.shard_id}</td>
                        <td className="p-3 text-white">{shard.collection}</td>
                        <td className="p-3 text-center">
                          {shard.is_leader ? (
                            <span className="px-2 py-1 bg-yellow-500/10 text-yellow-400 rounded text-xs">
                              Leader
                            </span>
                          ) : (
                            <span className="text-neutral-400">Follower</span>
                          )}
                        </td>
                        <td className="p-3 text-right text-white">
                      {formatNumber(shard.vector_count)}
                    </td>
                        <td className="p-3 text-right text-white">
                          {formatBytes(shard.size_bytes)}
                        </td>
                        <td className="p-3 text-center">
                          <span className={`px-2 py-1 rounded text-xs ${
                            shard.status === "healthy" ? "bg-green-500/10 text-green-400" :
                            shard.status === "degraded" ? "bg-yellow-500/10 text-yellow-400" :
                            "bg-red-500/10 text-red-400"
                          }`}>
                            {shard.status}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

const WorkersManagement: React.FC<WorkersManagementProps> = () => {
  const [workers, setWorkers] = useState<WorkerNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedWorker, setSelectedWorker] = useState<WorkerNode | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const formatCpuUsage = (value: unknown) =>
    formatNumber(toNumber(value, NaN), "--");

  const fetchWorkers = async () => {
    try {
      setError(null);
      const workersData = await managementApi.getWorkers();
      setWorkers(workersData);
    } catch (err) {
      setError("Failed to fetch worker nodes");
      console.error("Error fetching workers:", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchWorkers();
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;
    
    const interval = setInterval(() => {
      fetchWorkers();
    }, 30000);

    return () => clearInterval(interval);
  }, [autoRefresh]);

  const healthyWorkers = workers.filter(w => w.healthy).length;
  const totalVectors = workers.reduce((sum, w) => sum + (w.stats?.vector_count || 0), 0);
  const totalMemory = workers.reduce((sum, w) => sum + (w.stats?.memory_bytes || 0), 0);

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="flex items-center justify-center py-12">
          <RefreshCw className="w-8 h-8 text-purple-400 animate-spin" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="text-center py-12">
          <AlertCircle className="w-12 h-12 text-red-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-white mb-2">Failed to Load Workers</h2>
          <p className="text-neutral-400 mb-4">{error}</p>
          <button
            onClick={fetchWorkers}
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
            Workers Management
          </h1>
          <p className="text-neutral-400">
            Monitor and manage worker nodes across your Vectron deployment.
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
            onClick={fetchWorkers}
            className="flex items-center gap-2 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh
          </button>
        </div>
      </div>

      {/* Summary Statistics */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Server className="w-5 h-5 text-blue-400" />
            <h3 className="font-medium text-white">Total Workers</h3>
          </div>
          <div className="text-2xl font-bold text-white">{workers.length}</div>
          <p className="text-sm text-neutral-400">Worker Nodes</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <CheckCircle className="w-5 h-5 text-green-400" />
            <h3 className="font-medium text-white">Healthy</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {healthyWorkers}/{workers.length}
          </div>
          <p className="text-sm text-neutral-400">Healthy Workers</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Database className="w-5 h-5 text-purple-400" />
            <h3 className="font-medium text-white">Total Vectors</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatNumber(totalVectors, "0")}
          </div>
          <p className="text-sm text-neutral-400">Across All Workers</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <MemoryStick className="w-5 h-5 text-orange-400" />
            <h3 className="font-medium text-white">Memory Usage</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatBytes(totalMemory)}
          </div>
          <p className="text-sm text-neutral-400">Total Memory Used</p>
        </div>
      </div>

      {/* Worker Cards Grid */}
      {workers.length === 0 ? (
        <div className="text-center py-12">
          <Server className="w-12 h-12 text-neutral-500 mx-auto mb-4" />
          <h3 className="text-lg font-medium text-white mb-2">No Workers Found</h3>
          <p className="text-neutral-400">No worker nodes are currently registered</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {workers.map((worker) => {
            const healthState = getHealthState(worker);
            const isHealthy = healthState === "healthy";
            const isStarting = healthState === "starting";

            return (
            <div
              key={worker.worker_id}
              className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30 hover:bg-neutral-900/50 transition-all cursor-pointer"
              onClick={() => setSelectedWorker(worker)}
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className={`p-2 rounded-lg ${
                    isHealthy ? "bg-green-500/10" : isStarting ? "bg-yellow-500/10" : "bg-red-500/10"
                  }`}>
                    {isHealthy ? (
                      <CheckCircle className="w-5 h-5 text-green-400" />
                    ) : isStarting ? (
                      <AlertCircle className="w-5 h-5 text-yellow-400" />
                    ) : (
                      <XCircle className="w-5 h-5 text-red-400" />
                    )}
                  </div>
                  <div>
                    <h3 className="font-semibold text-white">{worker.worker_id}</h3>
                    <p className="text-sm text-neutral-400">{worker.grpc_address}</p>
                  </div>
                </div>
                
                <span className={`px-2 py-1 rounded text-xs font-medium ${
                  isHealthy
                    ? "bg-green-500/10 text-green-400"
                    : isStarting
                    ? "bg-yellow-500/10 text-yellow-400"
                    : "bg-red-500/10 text-red-400"
                }`}>
                  {isHealthy ? "HEALTHY" : isStarting ? "STARTING" : "UNHEALTHY"}
                </span>
              </div>

              {worker.stats && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-neutral-400">Vectors</span>
                    <span className="text-white font-medium">
                      {formatNumber(worker.stats.vector_count)}
                    </span>
                  </div>
                  
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-neutral-400">Collections</span>
                    <span className="text-white font-medium">
                      {(worker.collections ?? []).length}
                    </span>
                  </div>
                  
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-neutral-400">CPU</span>
                    <span className={`font-medium ${
                      worker.stats.cpu_usage > 80 ? "text-red-400" :
                      worker.stats.cpu_usage > 60 ? "text-yellow-400" : "text-green-400"
                    }`}>
                      {formatCpuUsage(worker.stats.cpu_usage)}%
                    </span>
                  </div>
                  
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-neutral-400">Memory</span>
                    <span className="text-white font-medium">
                      {formatBytes(worker.stats.memory_bytes)}
                    </span>
                  </div>
                  
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-neutral-400">Uptime</span>
                    <span className="text-white font-medium">
                      {formatUptime(worker.stats.uptime)}
                    </span>
                  </div>
                </div>
              )}

              {!worker.stats && (
                <div className="text-center py-4">
                  <p className="text-neutral-500 text-sm">No statistics available</p>
                </div>
              )}
              
              <div className="mt-4 pt-4 border-t border-neutral-800">
                <div className="flex flex-wrap gap-1">
                  {(worker.collections ?? []).slice(0, 2).map((collection) => (
                    <span
                      key={collection}
                      className="px-2 py-1 bg-purple-500/10 text-purple-400 rounded text-xs"
                    >
                      {collection}
                    </span>
                  ))}
                  {(worker.collections ?? []).length > 2 && (
                    <span className="px-2 py-1 bg-neutral-500/10 text-neutral-400 rounded text-xs">
                      +{(worker.collections ?? []).length - 2} more
                    </span>
                  )}
                  {(worker.collections ?? []).length === 0 && (
                    <span className="text-neutral-500 text-xs">No collections</span>
                  )}
                </div>
              </div>
            </div>
          );
          })}
        </div>
      )}

      {/* Worker Detail Modal */}
      {selectedWorker && (
        <WorkerModal
          worker={selectedWorker}
          onClose={() => setSelectedWorker(null)}
        />
      )}
    </div>
  );
};

export { WorkersManagement };
export default WorkersManagement;
