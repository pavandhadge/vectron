package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/crypto/bcrypt"

	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

const (
	apiKeyPrefix = "vectron/apikeys/"
	userPrefix   = "vectron/users/"
)

// CachedAPIKeyEntry holds cached API key validation results
type CachedAPIKeyEntry struct {
	Data      *APIKeyData
	Timestamp time.Time
}

// APIKeyCache provides TTL caching for validated API keys
type APIKeyCache struct {
	mu      sync.RWMutex
	entries map[string]*CachedAPIKeyEntry
	ttl     time.Duration
}

// NewAPIKeyCache creates a new API key cache
func NewAPIKeyCache(ttl time.Duration) *APIKeyCache {
	cache := &APIKeyCache{
		entries: make(map[string]*CachedAPIKeyEntry),
		ttl:     ttl,
	}
	go cache.cleanupLoop()
	return cache
}

// Get retrieves cached key data if not expired
func (c *APIKeyCache) Get(fullKey string) (*APIKeyData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[fullKey]
	if !exists {
		return nil, false
	}

	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Data, true
}

// Set stores validated key data in cache
func (c *APIKeyCache) Set(fullKey string, data *APIKeyData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[fullKey] = &CachedAPIKeyEntry{
		Data:      data,
		Timestamp: time.Now(),
	}
}

// Invalidate removes a key from cache
func (c *APIKeyCache) Invalidate(fullKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, fullKey)
}

// cleanupLoop periodically removes expired entries
func (c *APIKeyCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.Sub(entry.Timestamp) > c.ttl {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

// Client wraps the etcd client with caching.
type Client struct {
	*clientv3.Client
	apiKeyCache *APIKeyCache
}

// UserData is the structure for users stored in etcd.
type UserData struct {
	ID                 string                    `json:"id"`
	Email              string                    `json:"email"`
	HashedPassword     string                    `json:"hashed_password"`
	CreatedAt          int64                     `json:"created_at"`
	Plan               authpb.Plan               `json:"plan"`
	SubscriptionStatus authpb.SubscriptionStatus `json:"subscription_status"`
}

// APIKeyData is the structure for API keys stored in etcd.
type APIKeyData struct {
	HashedKey string      `json:"hashed_key"`
	UserID    string      `json:"user_id"`
	Name      string      `json:"name"`
	Plan      authpb.Plan `json:"plan"`
	CreatedAt int64       `json:"created_at"`
	KeyPrefix string      `json:"key_prefix"`
}

// NewClient creates a new etcd client with caching.
func NewClient(endpoints []string, timeout time.Duration) (*Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		Client:      cli,
		apiKeyCache: NewAPIKeyCache(5 * time.Minute), // Cache validated keys for 5 minutes
	}, nil
}

// --- User Management ---

// CreateUser stores a new user with a hashed password.
func (c *Client) CreateUser(ctx context.Context, email, password string) (*UserData, error) {
	// Check if user already exists
	if _, err := c.GetUserByEmail(ctx, email); err == nil {
		return nil, errors.New("user with this email already exists")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	userData := &UserData{
		ID:                 "user-" + uuid.New().String(),
		Email:              email,
		HashedPassword:     string(hashedPassword),
		CreatedAt:          time.Now().Unix(),
		Plan:               authpb.Plan_FREE,
		SubscriptionStatus: authpb.SubscriptionStatus_ACTIVE,
	}

	data, err := json.Marshal(userData)
	if err != nil {
		return nil, err
	}

	etcdKey := fmt.Sprintf("%s%s", userPrefix, userData.Email)
	_, err = c.Put(ctx, etcdKey, string(data))
	return userData, err
}

// GetUserByEmail retrieves a user by their email.
func (c *Client) GetUserByEmail(ctx context.Context, email string) (*UserData, error) {
	etcdKey := fmt.Sprintf("%s%s", userPrefix, email)
	resp, err := c.Get(ctx, etcdKey)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, errors.New("user not found")
	}

	var userData UserData
	if err := json.Unmarshal(resp.Kvs[0].Value, &userData); err != nil {
		return nil, err
	}
	return &userData, nil
}

// GetUserByID retrieves a user by their ID.
func (c *Client) GetUserByID(ctx context.Context, userID string) (*UserData, error) {
	// This is inefficient. In a real DB, you'd have an index on ID.
	// For etcd, we have to scan.
	resp, err := c.Get(ctx, userPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	for _, kv := range resp.Kvs {
		var userData UserData
		if err := json.Unmarshal(kv.Value, &userData); err == nil {
			if userData.ID == userID {
				return &userData, nil
			}
		}
	}
	return nil, errors.New("user not found")
}

// UpdateUserPlan updates the user's plan.
func (c *Client) UpdateUserPlan(ctx context.Context, userID string, plan authpb.Plan) (*UserData, error) {
	userData, err := c.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	userData.Plan = plan
	// If the plan is updated, we can assume the subscription is active.
	// In a real-world scenario, this would be more complex and likely handled by a billing event.
	if plan == authpb.Plan_PAID {
		userData.SubscriptionStatus = authpb.SubscriptionStatus_ACTIVE
	}

	data, err := json.Marshal(userData)
	if err != nil {
		return nil, err
	}

	etcdKey := fmt.Sprintf("%s%s", userPrefix, userData.Email)
	_, err = c.Put(ctx, etcdKey, string(data))
	return userData, err
}

// --- API Key Management ---

// CreateAPIKey generates a new key, hashes it, and stores it in etcd.
func (c *Client) CreateAPIKey(ctx context.Context, userID, name string) (string, *APIKeyData, error) {
	fullKey := "vkey-" + uuid.New().String()
	hashedKey, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, err
	}

	userData, err := c.GetUserByID(ctx, userID)
	if err != nil {
		return "", nil, err
	}

	keyInfo := &APIKeyData{
		HashedKey: string(hashedKey),
		UserID:    userID,
		Name:      name,
		Plan:      userData.Plan, // Use authpb.Plan
		CreatedAt: time.Now().Unix(),
		KeyPrefix: fullKey[:13], // "vkey-" + 8 chars of uuid
	}

	data, err := json.Marshal(keyInfo)
	if err != nil {
		return "", nil, err
	}

	etcdKey := fmt.Sprintf("%s%s", apiKeyPrefix, keyInfo.KeyPrefix)
	_, err = c.Put(ctx, etcdKey, string(data))
	if err != nil {
		return "", nil, err
	}

	return fullKey, keyInfo, nil
}

// ValidateAPIKey finds a key by its prefix and compares the full key with the stored hash.
// Uses caching to reduce etcd load for repeated validations.
func (c *Client) ValidateAPIKey(ctx context.Context, fullKey string) (*APIKeyData, bool, error) {
	if len(fullKey) < 13 {
		return nil, false, errors.New("invalid key format")
	}

	// Check cache first
	if cached, found := c.apiKeyCache.Get(fullKey); found {
		return cached, true, nil
	}

	prefix := fullKey[:13]
	etcdKey := fmt.Sprintf("%s%s", apiKeyPrefix, prefix)

	resp, err := c.Get(ctx, etcdKey)
	if err != nil {
		return nil, false, err
	}

	if len(resp.Kvs) == 0 {
		return nil, false, nil // Key not found
	}

	var keyData APIKeyData
	if err := json.Unmarshal(resp.Kvs[0].Value, &keyData); err != nil {
		return nil, false, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(keyData.HashedKey), []byte(fullKey)); err != nil {
		return nil, false, nil // Hash does not match
	}

	// Cache the successful validation
	c.apiKeyCache.Set(fullKey, &keyData)

	return &keyData, true, nil
}

// ListAPIKeys retrieves all keys for a given user ID.
func (c *Client) ListAPIKeys(ctx context.Context, userID string) ([]*authpb.APIKey, error) {
	resp, err := c.Get(ctx, apiKeyPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	var keys []*authpb.APIKey
	for _, kv := range resp.Kvs {
		var keyData APIKeyData
		if err := json.Unmarshal(kv.Value, &keyData); err == nil {
			if keyData.UserID == userID {
				keys = append(keys, &authpb.APIKey{
					KeyPrefix: keyData.KeyPrefix,
					UserId:    keyData.UserID,
					CreatedAt: keyData.CreatedAt,
					Name:      keyData.Name,
				})
			}
		}
	}
	return keys, nil
}

// DeleteAPIKey removes a key from etcd if the userID matches.
func (c *Client) DeleteAPIKey(ctx context.Context, keyPrefixToDelete, userID string) (bool, error) {
	etcdKey := fmt.Sprintf("%s%s", apiKeyPrefix, keyPrefixToDelete)

	resp, err := c.Get(ctx, etcdKey)
	if err != nil {
		return false, err
	}
	if len(resp.Kvs) == 0 {
		return false, errors.New("key not found")
	}

	var keyData APIKeyData
	if err := json.Unmarshal(resp.Kvs[0].Value, &keyData); err != nil {
		return false, err
	}

	if keyData.UserID != userID {
		return false, errors.New("user does not have permission to delete this key")
	}

	// Invalidate cache before deleting
	// We construct the full key to invalidate the cache entry
	fullKey := keyPrefixToDelete + keyData.HashedKey[:10] // Approximation for cache invalidation
	c.apiKeyCache.Invalidate(fullKey)

	_, err = c.Delete(ctx, etcdKey)
	return err == nil, err
}

// GetAPIKeyDataByPrefix retrieves APIKeyData by its prefix.
func (c *Client) GetAPIKeyDataByPrefix(ctx context.Context, prefix string) (*APIKeyData, error) {
	etcdKey := fmt.Sprintf("%s%s", apiKeyPrefix, prefix)
	resp, err := c.Get(ctx, etcdKey)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return nil, nil // Key not found
	}

	var keyData APIKeyData
	if err := json.Unmarshal(resp.Kvs[0].Value, &keyData); err != nil {
		return nil, err
	}
	return &keyData, nil
}

// --- User Account Deletion ---

// DeleteUser deletes a user and all associated API keys
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	// Get user data to find the email
	userData, err := c.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Delete user from etcd
	userKey := fmt.Sprintf("%s%s", userPrefix, userData.Email)
	_, err = c.Delete(ctx, userKey)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Delete all API keys associated with this user
	keys, err := c.ListAPIKeys(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to list user API keys: %w", err)
	}

	for _, key := range keys {
		keyKey := fmt.Sprintf("%s%s", apiKeyPrefix, key.KeyPrefix)
		_, err = c.Delete(ctx, keyKey)
		if err != nil {
			// Log but continue - don't fail if one key deletion fails
			fmt.Printf("Warning: failed to delete API key %s: %v\n", key.KeyPrefix, err)
		}
	}

	return nil
}
