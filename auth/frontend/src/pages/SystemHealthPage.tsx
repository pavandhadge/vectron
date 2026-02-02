import { useState, useEffect } from "react";
import {
  Activity,
  AlertTriangle,
  CheckCircle,
  RefreshCw,
  Server,
  Shield,
  Zap,
  XCircle,
  Eye,
  EyeOff,
  Filter,
  CheckSquare,
  Square,
} from "lucide-react";
import { SystemHealth } from "../api-types";
import { managementApi } from "../services/managementApi";

interface SystemHealthPageProps {}

const SystemHealthPage: React.FC<SystemHealthPageProps> = () => {
  const [systemHealth, setSystemHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [alertFilter, setAlertFilter] = useState<"all" | "active" | "resolved">("all");

  const fetchSystemHealth = async () => {
    try {
      setError(null);
      const health = await managementApi.getSystemHealth();
      setSystemHealth(health);
    } catch (err) {
      setError("Failed to fetch system health");
      console.error("Error fetching system health:", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSystemHealth();
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;
    
    const interval = setInterval(() => {
      fetchSystemHealth();
    }, 30000);

    return () => clearInterval(interval);
  }, [autoRefresh]);

  const resolveAlert = async (alertId: string) => {
    if (!systemHealth) return;
    
    try {
      // Call the API to persist the resolution
      await managementApi.resolveAlert(alertId);
      
      // Then update local state
      const updatedAlerts = systemHealth.alerts.map(alert =>
        alert.id === alertId ? { ...alert, resolved: true } : alert
      );
      
      setSystemHealth({
        ...systemHealth,
        alerts: updatedAlerts
      });
    } catch (err) {
      console.error("Failed to resolve alert:", err);
      setError("Failed to resolve alert. Please try again.");
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "up":
      case "healthy":
        return <CheckCircle className="w-5 h-5 text-green-400" />;
      case "degraded":
        return <AlertTriangle className="w-5 h-5 text-yellow-400" />;
      case "down":
      case "critical":
        return <XCircle className="w-5 h-5 text-red-400" />;
      default:
        return <RefreshCw className="w-5 h-5 text-neutral-400" />;
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case "up":
      case "healthy":
        return "text-green-400";
      case "degraded":
        return "text-yellow-400";
      case "down":
      case "critical":
        return "text-red-400";
      default:
        return "text-neutral-400";
    }
  };

  const getAlertLevelColor = (level: string) => {
    switch (level) {
      case "critical":
        return "bg-red-500/10 border-red-500/20 text-red-400";
      case "error":
        return "bg-red-500/10 border-red-500/20 text-red-400";
      case "warning":
        return "bg-yellow-500/10 border-yellow-500/20 text-yellow-400";
      case "info":
        return "bg-blue-500/10 border-blue-500/20 text-blue-400";
      default:
        return "bg-neutral-500/10 border-neutral-500/20 text-neutral-400";
    }
  };

  const filteredAlerts = systemHealth?.alerts.filter(alert => {
    if (alertFilter === "active") return !alert.resolved;
    if (alertFilter === "resolved") return alert.resolved;
    return true;
  }) || [];

  const formatBytes = (bytes: number) => {
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    if (bytes === 0) return "0 B";
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return Math.round(bytes / Math.pow(1024, i) * 100) / 100 + " " + sizes[i];
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

  if (error || !systemHealth) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="text-center py-12">
          <XCircle className="w-12 h-12 text-red-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-white mb-2">Failed to Load System Health</h2>
          <p className="text-neutral-400 mb-4">{error}</p>
          <button
            onClick={fetchSystemHealth}
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
            System Health
          </h1>
          <p className="text-neutral-400">
            Monitor system status, services, and alerts across your Vectron deployment.
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
            onClick={fetchSystemHealth}
            className="flex items-center gap-2 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh
          </button>
        </div>
      </div>

      {/* Overall Status */}
      <div className="rounded-xl border border-neutral-800 bg-neutral-900/50 p-6">
        <div className="flex items-center gap-4">
          <div className={`p-3 rounded-full ${getStatusColor(systemHealth.overall_status)}`}>
            {getStatusIcon(systemHealth.overall_status)}
          </div>
          <div>
            <h2 className="text-xl font-semibold text-white">System Status</h2>
            <p className={`text-lg font-medium ${getStatusColor(systemHealth.overall_status)}`}>
              {systemHealth.overall_status.charAt(0).toUpperCase() + systemHealth.overall_status.slice(1)}
            </p>
          </div>
        </div>
      </div>

      {/* Key Metrics */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Server className="w-5 h-5 text-purple-400" />
            <h3 className="font-medium text-white">Services</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {systemHealth.services.filter(s => s.status === "up").length}/{systemHealth.services.length}
          </div>
          <p className="text-sm text-neutral-400">Services Online</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Activity className="w-5 h-5 text-green-400" />
            <h3 className="font-medium text-white">Vectors</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {systemHealth.metrics.total_vectors.toLocaleString()}
          </div>
          <p className="text-sm text-neutral-400">Total Vectors</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Zap className="w-5 h-5 text-blue-400" />
            <h3 className="font-medium text-white">Workers</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {systemHealth.metrics.active_workers}
          </div>
          <p className="text-sm text-neutral-400">Active Workers</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Shield className="w-5 h-5 text-orange-400" />
            <h3 className="font-medium text-white">Storage</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatBytes(systemHealth.metrics.storage_used)}
          </div>
          <p className="text-sm text-neutral-400">Used Storage</p>
        </div>
      </div>

      {/* Services Status */}
      <div className="rounded-xl border border-neutral-800 bg-neutral-900/50">
        <div className="p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Service Status</h2>
        </div>
        
        <div className="divide-y divide-neutral-800">
          {systemHealth.services.map((service) => (
            <div key={service.name} className="p-6 flex items-center justify-between">
              <div className="flex items-center gap-4">
                {getStatusIcon(service.status)}
                <div>
                  <h3 className="font-medium text-white">{service.name}</h3>
                  <p className="text-sm text-neutral-400">{service.endpoint}</p>
                </div>
              </div>
              
              <div className="text-right">
                <div className={`text-sm font-medium ${getStatusColor(service.status)}`}>
                  {service.status.toUpperCase()}
                </div>
                <div className="text-xs text-neutral-500">
                  {service.response_time}ms response
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* System Alerts */}
      <div className="rounded-xl border border-neutral-800 bg-neutral-900/50">
        <div className="p-6 border-b border-neutral-800">
          <div className="flex items-center justify-between">
            <h2 className="text-xl font-semibold text-white">System Alerts</h2>
            
            <div className="flex items-center gap-2">
              <Filter className="w-4 h-4 text-neutral-400" />
              <select
                value={alertFilter}
                onChange={(e) => setAlertFilter(e.target.value as any)}
                className="bg-neutral-800 border border-neutral-700 rounded-lg px-3 py-1 text-sm text-white"
              >
                <option value="all">All Alerts</option>
                <option value="active">Active Only</option>
                <option value="resolved">Resolved Only</option>
              </select>
            </div>
          </div>
        </div>

        <div className="divide-y divide-neutral-800">
          {filteredAlerts.length === 0 ? (
            <div className="p-8 text-center">
              <CheckCircle className="w-12 h-12 text-green-400 mx-auto mb-4" />
              <h3 className="text-lg font-medium text-white mb-2">No Alerts</h3>
              <p className="text-neutral-400">
                {alertFilter === "active" ? "No active alerts" : alertFilter === "resolved" ? "No resolved alerts" : "All systems running smoothly"}
              </p>
            </div>
          ) : (
            filteredAlerts.map((alert) => (
              <div key={alert.id} className="p-6">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-start gap-4 flex-1">
                    <div className={`px-2 py-1 rounded text-xs font-medium border ${getAlertLevelColor(alert.level)}`}>
                      {alert.level.toUpperCase()}
                    </div>
                    
                    <div className="flex-1">
                      <h3 className="font-medium text-white mb-1">{alert.title}</h3>
                      <p className="text-sm text-neutral-400 mb-2">{alert.message}</p>
                      <div className="text-xs text-neutral-500">
                        {new Date(alert.timestamp).toLocaleString()} • {alert.source}
                      </div>
                    </div>
                  </div>
                  
                  <div className="flex items-center gap-2">
                    {alert.resolved ? (
                      <div className="flex items-center gap-1 text-green-400">
                        <CheckSquare className="w-4 h-4" />
                        <span className="text-sm">Resolved</span>
                      </div>
                    ) : (
                      <button
                        onClick={() => resolveAlert(alert.id)}
                        className="flex items-center gap-1 px-3 py-1 bg-purple-600 text-white text-sm rounded-lg hover:bg-purple-700 transition-colors"
                      >
                        <Square className="w-4 h-4" />
                        Resolve
                      </button>
                    )}
                  </div>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
};

export { SystemHealthPage };
export default SystemHealthPage;
