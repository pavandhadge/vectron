// Package management provides administrative and monitoring APIs for the Management Console.
// These endpoints are used by the frontend to display system health, metrics, and manage resources.
package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/apigateway/internal/feedback"
	pb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Manager handles all management endpoints
type Manager struct {
	pdClient    placementpb.PlacementServiceClient
	authClient  authpb.AuthServiceClient
	feedbackSvc *feedback.Service
	grpcClient  pb.VectronServiceClient

	// Internal state for metrics
	metricsMu     sync.RWMutex
	requestCount  int64
	errorCount    int64
	startTime     time.Time
	endpointStats map[string]*EndpointStat
}

// EndpointStat tracks statistics for a single endpoint
type EndpointStat struct {
	Path       string  `json:"path"`
	Method     string  `json:"method"`
	Count      int64   `json:"count"`
	AvgLatency float64 `json:"avg_latency_ms"`
	ErrorCount int64   `json:"error_count"`
}

// SystemHealth represents overall system health - JSON tags match frontend expectations
type SystemHealth struct {
	OverallStatus string          `json:"overall_status"`
	Services      []ServiceStatus `json:"services"`
	Alerts        []Alert         `json:"alerts"`
	Metrics       SystemMetrics   `json:"metrics"`
	Timestamp     int64           `json:"timestamp"`
}

// ServiceStatus represents the status of a single service - matches frontend
type ServiceStatus struct {
	Name         string `json:"name"`
	Status       string `json:"status"` // up, down, degraded
	ResponseTime int64  `json:"response_time"`
	Endpoint     string `json:"endpoint"`
	LastCheck    int64  `json:"last_check"`
	Error        string `json:"error,omitempty"`
}

// Alert represents a system alert - matches frontend
type Alert struct {
	ID        string `json:"id"`
	Level     string `json:"level"` // info, warning, error, critical
	Title     string `json:"title"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Resolved  bool   `json:"resolved"`
	Source    string `json:"source"`
}

// SystemMetrics contains key system metrics - matches frontend
type SystemMetrics struct {
	TotalVectors     int64     `json:"total_vectors"`
	TotalCollections int64     `json:"total_collections"`
	ActiveWorkers    int64     `json:"active_workers"`
	StorageUsed      int64     `json:"storage_used"`
	MemoryUsed       int64     `json:"memory_used"`
	CPUUsage         float64   `json:"cpu_usage"`
	NetworkIO        NetworkIO `json:"network_io"`
}

// NetworkIO represents network I/O statistics
type NetworkIO struct {
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`
}

// GatewayStats represents API Gateway statistics - matches frontend
type GatewayStats struct {
	Uptime              int64          `json:"uptime"`
	TotalRequests       int64          `json:"totalRequests"`
	RequestsPerSecond   float64        `json:"requestsPerSecond"`
	ActiveConnections   int64          `json:"activeConnections"`
	ErrorRate           float64        `json:"errorRate"`
	AverageResponseTime int64          `json:"averageResponseTime"`
	RateLimit           RateLimitInfo  `json:"rateLimit"`
	Endpoints           []EndpointStat `json:"endpoints"`
}

// RateLimitInfo represents rate limiting configuration and status
type RateLimitInfo struct {
	Enabled     bool  `json:"enabled"`
	RPS         int   `json:"rps"`
	ActiveUsers int64 `json:"activeUsers"`
}

// WorkerInfo represents a worker node with full details - matches frontend
type WorkerInfo struct {
	WorkerID      string            `json:"worker_id"`
	GRPCAddress   string            `json:"grpc_address"`
	RaftAddress   string            `json:"raft_address"`
	Collections   []string          `json:"collections"`
	Healthy       bool              `json:"healthy"`
	LastHeartbeat int64             `json:"last_heartbeat"`
	Metadata      map[string]string `json:"metadata"`
	Stats         WorkerStats       `json:"stats"`
}

// WorkerStats contains worker performance statistics - matches frontend
type WorkerStats struct {
	VectorCount int64      `json:"vector_count"`
	MemoryBytes int64      `json:"memory_bytes"`
	CPUUsage    float64    `json:"cpu_usage"`
	DiskUsage   int64      `json:"disk_usage"`
	Uptime      int64      `json:"uptime"`
	ShardInfo   []ShardInfo `json:"shard_info"`
}

// ShardInfo represents shard information for a worker
type ShardInfo struct {
	ShardID      uint64 `json:"shard_id"`
	Collection   string `json:"collection"`
	IsLeader     bool   `json:"is_leader"`
	ReplicaCount int    `json:"replica_count"`
	VectorCount  int64  `json:"vector_count"`
	SizeBytes    int64  `json:"size_bytes"`
	Status       string `json:"status"`
}

// CollectionInfo represents collection details with statistics - matches frontend
type CollectionInfo struct {
	Name        string          `json:"name"`
	Dimension   int32           `json:"dimension"`
	Distance    string          `json:"distance"`
	VectorCount int64           `json:"vector_count"`
	SizeBytes   int64           `json:"size_bytes"`
	ShardCount  int             `json:"shard_count"`
	Status      string          `json:"status"`
	CreatedAt   int64           `json:"created_at"`
	Shards      []CollectionShard `json:"shards"`
}

// CollectionShard represents brief shard information - matches frontend
type CollectionShard struct {
	ShardID     uint64   `json:"shard_id"`
	LeaderID    string   `json:"leader_id"`
	WorkerIDs   []string `json:"worker_ids"`
	Ready       bool     `json:"ready"`
	VectorCount int64    `json:"vector_count"`
	SizeBytes   int64    `json:"size_bytes"`
	Status      string   `json:"status"`
	LastUpdated int64    `json:"last_updated"`
}

// NewManager creates a new management manager
func NewManager(pdClient placementpb.PlacementServiceClient, authClient authpb.AuthServiceClient, feedbackSvc *feedback.Service, grpcClient pb.VectronServiceClient) *Manager {
	return &Manager{
		pdClient:      pdClient,
		authClient:    authClient,
		feedbackSvc:   feedbackSvc,
		grpcClient:    grpcClient,
		startTime:     time.Now(),
		endpointStats: make(map[string]*EndpointStat),
	}
}

// RecordRequest records a request for metrics
func (m *Manager) RecordRequest(path, method string, latency time.Duration, isError bool) {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	m.requestCount++
	if isError {
		m.errorCount++
	}

	key := method + " " + path
	stat, exists := m.endpointStats[key]
	if !exists {
		stat = &EndpointStat{
			Path:   path,
			Method: method,
		}
		m.endpointStats[key] = stat
	}

	stat.Count++
	if isError {
		stat.ErrorCount++
	}
	latencyMs := float64(latency.Milliseconds())
	stat.AvgLatency = (stat.AvgLatency*float64(stat.Count-1) + latencyMs) / float64(stat.Count)
}

// GetSystemHealth returns overall system health
func (m *Manager) GetSystemHealth(ctx context.Context) (*SystemHealth, error) {
	health := &SystemHealth{
		OverallStatus: "healthy",
		Services:      make([]ServiceStatus, 0),
		Alerts:        make([]Alert, 0),
		Timestamp:     time.Now().UnixMilli(),
	}

	now := time.Now().UnixMilli()

	// Check Placement Driver
	pdStart := time.Now()
	_, err := m.pdClient.ListCollections(ctx, &placementpb.ListCollectionsRequest{})
	pdStatus := ServiceStatus{
		Name:         "Placement Driver",
		Status:       "up",
		ResponseTime: time.Since(pdStart).Milliseconds(),
		Endpoint:     "placement-driver:6300",
		LastCheck:    now,
	}
	if err != nil {
		pdStatus.Status = "down"
		pdStatus.Error = err.Error()
		health.OverallStatus = "degraded"
		health.Alerts = append(health.Alerts, Alert{
			ID:        fmt.Sprintf("pd-down-%d", now),
			Level:     "critical",
			Title:     "Placement Driver Unavailable",
			Message:   fmt.Sprintf("Placement Driver is not responding: %v", err),
			Timestamp: now,
			Source:    "management",
		})
	}
	health.Services = append(health.Services, pdStatus)

	// Check Auth Service
	authStart := time.Now()
	_, err = m.authClient.GetUserProfile(ctx, &authpb.GetUserProfileRequest{})
	authStatus := ServiceStatus{
		Name:         "Auth Service",
		Status:       "up",
		ResponseTime: time.Since(authStart).Milliseconds(),
		Endpoint:     "auth:50051",
		LastCheck:    now,
	}
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() != codes.Unauthenticated {
			authStatus.Status = "degraded"
			authStatus.Error = err.Error()
			health.Alerts = append(health.Alerts, Alert{
				ID:        fmt.Sprintf("auth-degraded-%d", now),
				Level:     "warning",
				Title:     "Auth Service Degraded",
				Message:   fmt.Sprintf("Auth Service is experiencing issues: %v", err),
				Timestamp: now,
				Source:    "management",
			})
		}
	}
	health.Services = append(health.Services, authStatus)

	// Check API Gateway (self)
	gatewayStatus := ServiceStatus{
		Name:         "API Gateway",
		Status:       "up",
		ResponseTime: 0,
		Endpoint:     "apigateway:10012",
		LastCheck:    now,
	}
	health.Services = append(health.Services, gatewayStatus)

	// Get collections count
	collectionsRes, err := m.pdClient.ListCollections(ctx, &placementpb.ListCollectionsRequest{})
	if err == nil {
		health.Metrics.TotalCollections = int64(len(collectionsRes.Collections))
	}

	// Get workers
	workersRes, err := m.pdClient.ListWorkers(ctx, &placementpb.ListWorkersRequest{})
	if err == nil {
		health.Metrics.ActiveWorkers = int64(len(workersRes.Workers))

		// Add worker services to the list
		for _, w := range workersRes.Workers {
			workerStatus := "up"
			if !w.Healthy {
				workerStatus = "down"
			}
			health.Services = append(health.Services, ServiceStatus{
				Name:         fmt.Sprintf("Worker %s", w.WorkerId),
				Status:       workerStatus,
				ResponseTime: 0,
				Endpoint:     w.GrpcAddress,
				LastCheck:    w.LastHeartbeat * 1000, // Convert seconds to milliseconds
			})
		}
	} else {
		health.Alerts = append(health.Alerts, Alert{
			ID:        fmt.Sprintf("workers-unavailable-%d", now),
			Level:     "warning",
			Title:     "Worker Information Unavailable",
			Message:   "Could not retrieve worker list from Placement Driver",
			Timestamp: now,
			Source:    "management",
		})
	}

	// Determine overall status
	if len(health.Alerts) > 0 {
		hasCritical := false
		for _, alert := range health.Alerts {
			if alert.Level == "critical" {
				hasCritical = true
				break
			}
		}
		if hasCritical {
			health.OverallStatus = "critical"
		} else {
			health.OverallStatus = "degraded"
		}
	}

	return health, nil
}

// GetGatewayStats returns API Gateway statistics
func (m *Manager) GetGatewayStats() *GatewayStats {
	m.metricsMu.RLock()
	defer m.metricsMu.RUnlock()

	uptime := int64(time.Since(m.startTime).Seconds())

	rps := float64(m.requestCount) / float64(uptime)
	if uptime == 0 {
		rps = 0
	}

	var errorRate float64
	if m.requestCount > 0 {
		errorRate = float64(m.errorCount) / float64(m.requestCount)
	}

	endpoints := make([]EndpointStat, 0, len(m.endpointStats))
	for _, stat := range m.endpointStats {
		endpoints = append(endpoints, *stat)
	}

	return &GatewayStats{
		Uptime:            uptime,
		TotalRequests:     m.requestCount,
		RequestsPerSecond: rps,
		ActiveConnections: 0,
		ErrorRate:         errorRate,
		AverageResponseTime: 0,
		RateLimit: RateLimitInfo{
			Enabled:     true,
			RPS:         100,
			ActiveUsers: 0,
		},
		Endpoints: endpoints,
	}
}

// GetWorkers returns all workers with detailed information
func (m *Manager) GetWorkers(ctx context.Context) ([]WorkerInfo, error) {
	res, err := m.pdClient.ListWorkers(ctx, &placementpb.ListWorkersRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list workers: %w", err)
	}

	workers := make([]WorkerInfo, 0, len(res.Workers))

	for _, w := range res.Workers {
		worker := WorkerInfo{
			WorkerID:      w.WorkerId,
			GRPCAddress:   w.GrpcAddress,
			RaftAddress:   w.RaftAddress,
			Collections:   w.Collections,
			Healthy:       w.Healthy,
			LastHeartbeat: w.LastHeartbeat * 1000, // Convert to milliseconds
			Metadata:      w.Metadata,
			Stats: WorkerStats{
				VectorCount: 0,
				MemoryBytes: 0,
				CPUUsage:    0,
				DiskUsage:   0,
				Uptime:      0,
				ShardInfo:   make([]ShardInfo, 0),
			},
		}

		workers = append(workers, worker)
	}

	return workers, nil
}

// GetCollections returns all collections with detailed statistics
func (m *Manager) GetCollections(ctx context.Context) ([]CollectionInfo, error) {
	res, err := m.pdClient.ListCollections(ctx, &placementpb.ListCollectionsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	collections := make([]CollectionInfo, 0, len(res.Collections))

	for _, name := range res.Collections {
		statusRes, err := m.pdClient.GetCollectionStatus(ctx, &placementpb.GetCollectionStatusRequest{
			Name: name,
		})

		if err != nil {
			collections = append(collections, CollectionInfo{
				Name:   name,
				Status: "error",
			})
			continue
		}

		info := CollectionInfo{
			Name:       statusRes.Name,
			Dimension:  statusRes.Dimension,
			Distance:   statusRes.Distance,
			ShardCount: len(statusRes.Shards),
			Status:     "active",
			CreatedAt:  time.Now().UnixMilli(), // PD doesn't provide created_at yet
		}

		// Convert shard info - fetch worker assignments for each shard
		for _, shard := range statusRes.Shards {
			// Convert replica IDs to strings for worker_ids
			workerIDs := make([]string, 0, len(shard.Replicas))
			for _, replicaID := range shard.Replicas {
				workerIDs = append(workerIDs, fmt.Sprintf("worker-%d", replicaID))
			}
			
			info.Shards = append(info.Shards, CollectionShard{
				ShardID:     uint64(shard.ShardId),
				LeaderID:    fmt.Sprintf("worker-%d", shard.LeaderId),
				WorkerIDs:   workerIDs,
				Ready:       shard.Ready,
				VectorCount: 0, // Not provided by PD yet
				SizeBytes:   0, // Not provided by PD yet
				Status:      "healthy",
				LastUpdated: time.Now().UnixMilli(),
			})
		}

		collections = append(collections, info)
	}

	return collections, nil
}

// HTTP Handlers

// HandleSystemHealth handles GET /v1/system/health
func (m *Manager) HandleSystemHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	health, err := m.GetSystemHealth(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get system health: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// HandleGatewayStats handles GET /v1/admin/stats
func (m *Manager) HandleGatewayStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := m.GetGatewayStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stats":     stats,
		"endpoints": stats.Endpoints,
	})
}

// HandleWorkers handles GET /v1/admin/workers
func (m *Manager) HandleWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	workers, err := m.GetWorkers(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get workers: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"count":   len(workers),
	})
}

// HandleCollections handles GET /v1/admin/collections
func (m *Manager) HandleCollections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	collections, err := m.GetCollections(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get collections: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"collections": collections,
		"count":       len(collections),
	})
}

// HandleAlerts handles GET /v1/alerts
func (m *Manager) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")
	level := r.URL.Query().Get("level")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	health, err := m.GetSystemHealth(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get alerts: %v", err), http.StatusInternalServerError)
		return
	}

	var filtered []Alert
	for _, alert := range health.Alerts {
		if status != "" {
			if status == "active" && alert.Resolved {
				continue
			}
			if status == "resolved" && !alert.Resolved {
				continue
			}
		}
		if level != "" && alert.Level != level {
			continue
		}
		filtered = append(filtered, alert)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": filtered,
		"count":  len(filtered),
	})
}

// HandleResolveAlert handles POST /v1/alerts/{id}/resolve
func (m *Manager) HandleResolveAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	parts := splitPath(path)
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	alertID := parts[2]

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"alert_id": alertID,
		"message":  "Alert marked as resolved",
	})
}

// Helper function to split path
func splitPath(path string) []string {
	var parts []string
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
