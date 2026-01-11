// src/pages/dashboard.mock.ts

import { Database, Activity, Zap } from "lucide-react";

export const dashboardStats = [
  {
    title: "Total Vectors",
    value: "1,240,592",
    subtext: "Across 4 collections",
    icon: Database,
    trend: 12.5,
  },
  {
    title: "Requests (24h)",
    value: "84.3k",
    subtext: "Avg. 3.2k / hour",
    icon: Activity,
    trend: -2.4,
  },
  {
    title: "Avg. Latency",
    value: "14ms",
    subtext: "P99: 22ms",
    icon: Zap,
    trend: 5.1,
  },
];

export const requestVolumeBars = [
  40, 65, 45, 80, 55, 90, 70, 85, 60, 75, 50, 95,
];

export const recentActivity = [
  {
    action: "Created collection 'users_v2'",
    time: "2m ago",
    user: "You",
  },
  {
    action: "API Key rotated",
    time: "1h ago",
    user: "System",
  },
  {
    action: "High latency alert resolved",
    time: "4h ago",
    user: "Monitor",
  },
  {
    action: "Backup completed",
    time: "1d ago",
    user: "System",
  },
];
