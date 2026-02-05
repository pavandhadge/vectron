export const toNumber = (value: unknown, fallback = 0): number => {
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : fallback;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

export const toStringSafe = (value: unknown, fallback = ""): string => {
  if (typeof value === "string") return value;
  if (value === null || value === undefined) return fallback;
  return String(value);
};

export const toLowerSafe = (value: unknown): string => toStringSafe(value).toLowerCase();

export const formatNumber = (value: unknown, fallback = "--"): string => {
  const num = toNumber(value, NaN);
  if (!Number.isFinite(num)) return fallback;
  return num.toLocaleString();
};

export const formatCompactNumber = (value: unknown, fallback = "--"): string => {
  const num = toNumber(value, NaN);
  if (!Number.isFinite(num)) return fallback;
  if (num >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`;
  if (num >= 1_000) return `${(num / 1_000).toFixed(1)}K`;
  return num.toString();
};

export const formatBytes = (value: unknown, fallback = "--"): string => {
  const bytes = toNumber(value, NaN);
  if (!Number.isFinite(bytes) || bytes < 0) return fallback;
  if (bytes === 0) return "0 B";
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), sizes.length - 1);
  const rounded = Math.round((bytes / Math.pow(1024, i)) * 100) / 100;
  return `${rounded} ${sizes[i]}`;
};

export const formatUptime = (value: unknown, fallback = "--"): string => {
  const seconds = toNumber(value, NaN);
  if (!Number.isFinite(seconds) || seconds < 0) return fallback;
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (days > 0) return `${days}d ${hours}h ${minutes}m`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
};

export const formatDateTime = (value: unknown, fallback = "--"): string => {
  const ts = toNumber(value, NaN);
  if (!Number.isFinite(ts) || ts <= 0) return fallback;
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return fallback;
  return date.toLocaleString();
};

export const formatTime = (value: unknown, fallback = "--"): string => {
  const ts = toNumber(value, NaN);
  if (!Number.isFinite(ts) || ts <= 0) return fallback;
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return fallback;
  return date.toLocaleTimeString();
};

export const formatPercent = (value: unknown, digits = 2, fallback = "--"): string => {
  const num = toNumber(value, NaN);
  if (!Number.isFinite(num)) return fallback;
  return `${num.toFixed(digits)}%`;
};
