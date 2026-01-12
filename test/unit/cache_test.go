package unit

import (
	"fmt"
	"testing"
	"time"

	"github.com/LENAX/task-engine/pkg/core/cache"
)

// TestMemoryResultCache_SetAndGet 测试缓存设置和获取
func TestMemoryResultCache_SetAndGet(t *testing.T) {
	c := cache.NewMemoryResultCache()

	// 设置缓存
	taskID := "test-task"
	result := "test result"
	ttl := 1 * time.Hour

	err := c.Set(taskID, result, ttl)
	if err != nil {
		t.Fatalf("设置缓存失败: %v", err)
	}

	// 获取缓存
	cached, found := c.Get(taskID)
	if !found {
		t.Error("期望缓存存在，但未找到")
	}

	if cached != result {
		t.Errorf("期望缓存值为'%s'，实际为'%v'", result, cached)
	}
}

// TestMemoryResultCache_TTLExpiration 测试缓存TTL过期
func TestMemoryResultCache_TTLExpiration(t *testing.T) {
	c := cache.NewMemoryResultCache()

	// 设置短期缓存
	taskID := "test-task"
	result := "test result"
	ttl := 100 * time.Millisecond

	err := c.Set(taskID, result, ttl)
	if err != nil {
		t.Fatalf("设置缓存失败: %v", err)
	}

	// 立即获取应该存在
	_, found := c.Get(taskID)
	if !found {
		t.Error("期望缓存存在，但未找到")
	}

	// 等待过期
	time.Sleep(150 * time.Millisecond)

	// 再次获取应该不存在
	_, found = c.Get(taskID)
	if found {
		t.Error("期望缓存已过期，但仍然存在")
	}
}

// TestMemoryResultCache_ConcurrentAccess 测试缓存并发安全
func TestMemoryResultCache_ConcurrentAccess(t *testing.T) {
	c := cache.NewMemoryResultCache()

	// 并发设置和获取
	concurrency := 100
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			taskID := fmt.Sprintf("task-%d", idx)
			result := fmt.Sprintf("result-%d", idx)
			ttl := 1 * time.Hour

			// 设置
			if err := c.Set(taskID, result, ttl); err != nil {
				t.Errorf("设置缓存失败: %v", err)
			}

			// 获取
			cached, found := c.Get(taskID)
			if !found {
				t.Errorf("期望缓存存在")
			}
			if cached != result {
				t.Errorf("期望缓存值为'%s'，实际为'%v'", result, cached)
			}

			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < concurrency; i++ {
		<-done
	}
}

// TestMemoryResultCache_Delete 测试删除缓存
func TestMemoryResultCache_Delete(t *testing.T) {
	c := cache.NewMemoryResultCache()

	taskID := "test-task"
	result := "test result"
	ttl := 1 * time.Hour

	// 设置缓存
	c.Set(taskID, result, ttl)

	// 删除缓存
	err := c.Delete(taskID)
	if err != nil {
		t.Fatalf("删除缓存失败: %v", err)
	}

	// 验证已删除
	_, found := c.Get(taskID)
	if found {
		t.Error("期望缓存已删除，但仍然存在")
	}
}

// TestMemoryResultCache_Clear 测试清空所有缓存
func TestMemoryResultCache_Clear(t *testing.T) {
	c := cache.NewMemoryResultCache()

	// 设置多个缓存
	for i := 0; i < 10; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		c.Set(taskID, fmt.Sprintf("result-%d", i), 1*time.Hour)
	}

	// 清空所有缓存
	err := c.Clear()
	if err != nil {
		t.Fatalf("清空缓存失败: %v", err)
	}

	// 验证所有缓存都已清空
	for i := 0; i < 10; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		_, found := c.Get(taskID)
		if found {
			t.Errorf("期望缓存 %s 已清空，但仍然存在", taskID)
		}
	}
}

