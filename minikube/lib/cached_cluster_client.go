package lib

import (
	"sync"
	"time"

	"k8s.io/minikube/pkg/minikube/config"
	"k8s.io/minikube/pkg/minikube/kubeconfig"
)

// CachedClusterClient wraps a ClusterClient and provides caching for expensive operations
type CachedClusterClient struct {
	ClusterClient

	// Cache for cluster config
	configCache      *config.ClusterConfig
	configCacheTime  time.Time
	configCacheTTL   time.Duration
	configCacheMutex sync.RWMutex

	// Cache for addons
	addonsCache      []string
	addonsCacheTime  time.Time
	addonsCacheTTL   time.Duration
	addonsCacheMutex sync.RWMutex
}

// NewCachedClusterClient creates a new cached cluster client
func NewCachedClusterClient(client ClusterClient) *CachedClusterClient {
	return &CachedClusterClient{
		ClusterClient:  client,
		configCacheTTL: 30 * time.Second, // Cache cluster config for 30 seconds
		addonsCacheTTL: 30 * time.Second, // Cache addons for 30 seconds
	}
}

// GetClusterConfig retrieves the cluster config with caching
func (c *CachedClusterClient) GetClusterConfig() *config.ClusterConfig {
	c.configCacheMutex.RLock()
	if c.configCache != nil && time.Since(c.configCacheTime) < c.configCacheTTL {
		defer c.configCacheMutex.RUnlock()
		return c.configCache
	}
	c.configCacheMutex.RUnlock()

	// Cache miss or expired, fetch fresh data
	c.configCacheMutex.Lock()
	defer c.configCacheMutex.Unlock()

	// Double-check in case another goroutine updated the cache
	if c.configCache != nil && time.Since(c.configCacheTime) < c.configCacheTTL {
		return c.configCache
	}

	// Fetch fresh data
	c.configCache = c.ClusterClient.GetClusterConfig()
	c.configCacheTime = time.Now()
	return c.configCache
}

// GetAddons retrieves addons with caching
func (c *CachedClusterClient) GetAddons() []string {
	c.addonsCacheMutex.RLock()
	if c.addonsCache != nil && time.Since(c.addonsCacheTime) < c.addonsCacheTTL {
		defer c.addonsCacheMutex.RUnlock()
		return c.addonsCache
	}
	c.addonsCacheMutex.RUnlock()

	// Cache miss or expired, fetch fresh data
	c.addonsCacheMutex.Lock()
	defer c.addonsCacheMutex.Unlock()

	// Double-check in case another goroutine updated the cache
	if c.addonsCache != nil && time.Since(c.addonsCacheTime) < c.addonsCacheTTL {
		return c.addonsCache
	}

	// Fetch fresh data
	c.addonsCache = c.ClusterClient.GetAddons()
	c.addonsCacheTime = time.Now()
	return c.addonsCache
}

// InvalidateCache clears all caches
func (c *CachedClusterClient) InvalidateCache() {
	c.configCacheMutex.Lock()
	c.configCache = nil
	c.configCacheMutex.Unlock()

	c.addonsCacheMutex.Lock()
	c.addonsCache = nil
	c.addonsCacheMutex.Unlock()
}

// Start invalidates cache and delegates to underlying client
func (c *CachedClusterClient) Start() (*kubeconfig.Settings, error) {
	c.InvalidateCache()
	return c.ClusterClient.Start()
}

// ApplyAddons invalidates cache and delegates to underlying client
func (c *CachedClusterClient) ApplyAddons(addons []string) error {
	c.InvalidateCache()
	return c.ClusterClient.ApplyAddons(addons)
}

// Delete invalidates cache and delegates to underlying client
func (c *CachedClusterClient) Delete() error {
	c.InvalidateCache()
	return c.ClusterClient.Delete()
}

