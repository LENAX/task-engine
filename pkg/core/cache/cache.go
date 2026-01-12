package cache

import (
	"sync"
	"time"
)

// ResultCache 结果缓存接口（对外导出）
type ResultCache interface {
	// Set 设置缓存值
	// taskID: 任务ID
	// result: 结果数据
	// ttl: 缓存有效期（秒）
	Set(taskID string, result interface{}, ttl time.Duration) error

	// Get 获取缓存值
	// taskID: 任务ID
	// 返回: 结果数据和是否存在
	Get(taskID string) (interface{}, bool)

	// Delete 删除缓存值
	// taskID: 任务ID
	Delete(taskID string) error

	// Clear 清空所有缓存
	Clear() error
}

// cacheEntry 缓存条目（内部使用）
type cacheEntry struct {
	value      interface{}
	expireTime time.Time
}

// MemoryResultCache 内存结果缓存实现（对外导出）
type MemoryResultCache struct {
	mu    sync.RWMutex
	cache map[string]*cacheEntry
}

// NewMemoryResultCache 创建内存结果缓存实例（对外导出）
func NewMemoryResultCache() *MemoryResultCache {
	c := &MemoryResultCache{
		cache: make(map[string]*cacheEntry),
	}
	// 启动清理协程，定期清理过期缓存
	go c.cleanupExpired()
	return c
}

// Set 设置缓存值
func (c *MemoryResultCache) Set(taskID string, result interface{}, ttl time.Duration) error {
	if taskID == "" {
		return nil // 空key，忽略
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	expireTime := time.Now().Add(ttl)
	c.cache[taskID] = &cacheEntry{
		value:      result,
		expireTime: expireTime,
	}

	return nil
}

// Get 获取缓存值
func (c *MemoryResultCache) Get(taskID string) (interface{}, bool) {
	if taskID == "" {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.cache[taskID]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if time.Now().After(entry.expireTime) {
		// 已过期，删除并返回不存在
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.cache, taskID)
		c.mu.Unlock()
		c.mu.RLock()
		return nil, false
	}

	return entry.value, true
}

// Delete 删除缓存值
func (c *MemoryResultCache) Delete(taskID string) error {
	if taskID == "" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, taskID)
	return nil
}

// Clear 清空所有缓存
func (c *MemoryResultCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheEntry)
	return nil
}

// cleanupExpired 清理过期缓存（内部方法）
func (c *MemoryResultCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute) // 每分钟清理一次
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.cache {
			if now.After(entry.expireTime) {
				delete(c.cache, key)
			}
		}
		c.mu.Unlock()
	}
}

