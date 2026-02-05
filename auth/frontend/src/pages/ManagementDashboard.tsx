import { useState, useEffect } from 'react';
import {
  Activity,
  Server,
  Database,
  AlertTriangle,
  CheckCircle,
  XCircle,
  TrendingUp,
  Cpu,
  HardDrive,
  Wifi,
} from 'lucide-react';
import type { SystemHealth } from '../api-types';
import { managementApi } from '../services/managementApi';
import {
  formatBytes,
  formatCompactNumber,
  formatNumber,
  formatTime,
  toNumber,
} from '../utils/format';

interface StatCardProps {
  title: string;
  value: string | number;
  change?: string;
  icon: React.ElementType;
  trend?: 'up' | 'down' | 'neutral';
}

const StatCard = ({ title, value, change, icon: Icon, trend = 'neutral' }: StatCardProps) => (
  <div className="bg-neutral-900/50 border border-neutral-800 rounded-lg p-6 hover:bg-neutral-900/70 transition-colors">
    <div className="flex items-center justify-between mb-4">
      <div className="flex items-center gap-3">
        <div className="p-2 rounded-lg bg-purple-500/10 text-purple-400">
          <Icon className="w-5 h-5" />
        </div>
        <h3 className="text-sm font-medium text-neutral-400">{title}</h3>
      </div>
      {trend !== 'neutral' && (
        <div className={`flex items-center gap-1 text-xs ${
          trend === 'up' ? 'text-green-400' : 'text-red-400'
        }`}>
          <TrendingUp className={`w-3 h-3 ${
            trend === 'down' ? 'rotate-180' : ''
          }`} />
          {change}
        </div>
      )}
    </div>
    <div className="text-2xl font-bold text-white">{value}</div>
  </div>
);

interface ServiceStatusProps {
  name: string;
  status: 'up' | 'down' | 'degraded';
  endpoint: string;
  responseTime: number;
  lastCheck: number;
}

const ServiceStatusCard = ({
  name,
  status,
  endpoint: _endpoint,
  responseTime,
  lastCheck,
}: ServiceStatusProps) => {
  const statusConfig = {
    up: { icon: CheckCircle, color: 'text-green-400', bg: 'bg-green-500/10' },
    down: { icon: XCircle, color: 'text-red-400', bg: 'bg-red-500/10' },
    degraded: { icon: AlertTriangle, color: 'text-yellow-400', bg: 'bg-yellow-500/10' },
  };

  const config = statusConfig[status];
  const Icon = config.icon;

  return (
    <div className="bg-neutral-900/50 border border-neutral-800 rounded-lg p-4">
      <div className="flex items-center justify-between mb-2">
        <h4 className="font-medium text-white">{name}</h4>
        <div className={`p-1 rounded ${config.bg}`}>
          <Icon className={`w-4 h-4 ${config.color}`} />
        </div>
      </div>
      <div className="text-sm text-neutral-400 space-y-1">
        <div className="flex justify-between">
          <span>Status:</span>
          <span className={config.color}>{status.toUpperCase()}</span>
        </div>
        <div className="flex justify-between">
          <span>Response:</span>
          <span>{formatNumber(responseTime, "0")}ms</span>
        </div>
        <div className="flex justify-between">
          <span>Last Check:</span>
          <span>{formatTime(lastCheck)}</span>
        </div>
      </div>
    </div>
  );
};

export const ManagementDashboard = () => {
  const [systemHealth, setSystemHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState<ReturnType<typeof setInterval> | null>(null);

  const fetchData = async () => {
    try {
      const health = await managementApi.getSystemHealth();
      setSystemHealth(health);
    } catch (error) {
      console.error('Failed to fetch system health:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
    
    // Set up auto-refresh every 30 seconds
    const interval = setInterval(fetchData, 30000);
    setRefreshInterval(interval);

    return () => {
      if (refreshInterval) clearInterval(refreshInterval);
      clearInterval(interval);
    };
  }, []);

  const cpuUsage = Math.min(Math.max(toNumber(systemHealth?.metrics.cpu_usage, 0), 0), 100);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-purple-500" />
      </div>
    );
  }

  if (!systemHealth) {
    return (
      <div className="min-h-screen flex items-center justify-center text-neutral-400">
        <div className="text-center">
          <XCircle className="w-12 h-12 mx-auto mb-4 text-red-400" />
          <p>Failed to load system health data</p>
        </div>
      </div>
    );
  }

  const { metrics, services, alerts, overall_status } = systemHealth;

  return (
    <div className="p-6 space-y-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white mb-2">Management Console</h1>
          <p className="text-neutral-400">Monitor and manage your Vectron database cluster</p>
        </div>
        <div className="flex items-center gap-3">
          <div className={`px-3 py-2 rounded-full border text-sm font-medium ${
            overall_status === 'healthy'
              ? 'border-green-500/20 bg-green-500/10 text-green-400'
              : overall_status === 'degraded'
              ? 'border-yellow-500/20 bg-yellow-500/10 text-yellow-400'
              : 'border-red-500/20 bg-red-500/10 text-red-400'
          }`}>
            System {overall_status.toUpperCase()}
          </div>
          <button
            onClick={fetchData}
            className="px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white rounded-lg transition-colors"
          >
            Refresh
          </button>
        </div>
      </div>

      {/* Key Metrics */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <StatCard
          title="Total Vectors"
          value={formatCompactNumber(metrics.total_vectors)}
          icon={Database}
          trend="up"
          change="+2.5%"
        />
        <StatCard
          title="Collections"
          value={formatNumber(metrics.total_collections, "0")}
          icon={Server}
          trend="neutral"
        />
        <StatCard
          title="Active Workers"
          value={formatNumber(metrics.active_workers, "0")}
          icon={Activity}
          trend="neutral"
        />
        <StatCard
          title="Storage Used"
          value={formatBytes(metrics.storage_used)}
          icon={HardDrive}
          trend="up"
          change="+1.2%"
        />
      </div>

      {/* System Resources */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="bg-neutral-900/50 border border-neutral-800 rounded-lg p-6">
          <div className="flex items-center gap-3 mb-4">
            <Cpu className="w-5 h-5 text-purple-400" />
            <h3 className="text-lg font-semibold text-white">CPU Usage</h3>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-neutral-400">Current</span>
              <span className="text-white font-medium">
                {formatNumber(cpuUsage, "0")}%
              </span>
            </div>
            <div className="w-full bg-neutral-800 rounded-full h-2">
              <div
                className={`h-2 rounded-full ${
                  cpuUsage > 80 ? 'bg-red-500' : cpuUsage > 60 ? 'bg-yellow-500' : 'bg-green-500'
                }`}
                style={{ width: `${cpuUsage}%` }}
              />
            </div>
          </div>
        </div>

        <div className="bg-neutral-900/50 border border-neutral-800 rounded-lg p-6">
          <div className="flex items-center gap-3 mb-4">
            <HardDrive className="w-5 h-5 text-purple-400" />
            <h3 className="text-lg font-semibold text-white">Memory Usage</h3>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-neutral-400">Used</span>
              <span className="text-white font-medium">
                {formatBytes(metrics.memory_used)}
              </span>
            </div>
          </div>
        </div>

        <div className="bg-neutral-900/50 border border-neutral-800 rounded-lg p-6">
          <div className="flex items-center gap-3 mb-4">
            <Wifi className="w-5 h-5 text-purple-400" />
            <h3 className="text-lg font-semibold text-white">Network I/O</h3>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-neutral-400">In</span>
              <span className="text-white font-medium">
                {formatBytes(metrics.network_io?.bytes_in)}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-neutral-400">Out</span>
              <span className="text-white font-medium">
                {formatBytes(metrics.network_io?.bytes_out)}
              </span>
            </div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Services Status */}
        <div className="bg-neutral-900/30 border border-neutral-800 rounded-lg p-6">
          <h3 className="text-lg font-semibold text-white mb-4">Service Status</h3>
          <div className="space-y-4">
            {services.map((service, index) => (
              <ServiceStatusCard
                key={index}
                name={service.name}
                status={service.status}
                endpoint={service.endpoint}
                responseTime={service.response_time}
                lastCheck={service.last_check}
              />
            ))}
          </div>
        </div>

        {/* Recent Alerts */}
        <div className="bg-neutral-900/30 border border-neutral-800 rounded-lg p-6">
          <h3 className="text-lg font-semibold text-white mb-4">Recent Alerts</h3>
          <div className="space-y-3">
            {alerts.length === 0 ? (
              <div className="text-center py-8 text-neutral-400">
                <CheckCircle className="w-12 h-12 mx-auto mb-3 text-green-400" />
                <p>No active alerts</p>
              </div>
            ) : (
              alerts.map((alert) => {
                const levelConfig = {
                  info: 'border-blue-500/20 bg-blue-500/10 text-blue-400',
                  warning: 'border-yellow-500/20 bg-yellow-500/10 text-yellow-400',
                  error: 'border-red-500/20 bg-red-500/10 text-red-400',
                  critical: 'border-red-500/30 bg-red-500/20 text-red-300',
                };

                return (
                  <div
                    key={alert.id}
                    className={`border rounded-lg p-4 ${levelConfig[alert.level]}`}
                  >
                    <div className="flex items-start justify-between mb-2">
                      <h4 className="font-medium">{alert.title}</h4>
                      <span className="text-xs opacity-70">
                        {formatTime(alert.timestamp)}
                      </span>
                    </div>
                    <p className="text-sm opacity-90 mb-2">{alert.message}</p>
                    <div className="flex items-center justify-between text-xs">
                      <span>Source: {alert.source}</span>
                      {!alert.resolved && (
                        <span className="px-2 py-1 bg-current/20 rounded text-current">
                          ACTIVE
                        </span>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default ManagementDashboard;
