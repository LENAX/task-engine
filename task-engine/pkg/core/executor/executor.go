package executor

import (
	"log"
	"sync"
)

// Executor 执行器核心结构体（对外导出）
type Executor struct {
	maxWorkers int
	workerPool chan struct{}
	wg         sync.WaitGroup
	running    bool
}

// NewExecutor 创建执行器实例（对外导出的工厂方法，engine包会调用）
func NewExecutor(maxWorkers int) (*Executor, error) {
	if maxWorkers <= 0 {
		maxWorkers = 10 // 默认值
	}
	return &Executor{
		maxWorkers: maxWorkers,
		workerPool: make(chan struct{}, maxWorkers),
		running:    false,
	}, nil
}

// Shutdown 关闭执行器（对外导出）
func (e *Executor) Shutdown() {
	if !e.running {
		return
	}
	e.running = false
	close(e.workerPool)
	e.wg.Wait()
	log.Println("✅ 执行器已关闭")
}

// Start 启动执行器（对外导出）
func (e *Executor) Start() {
	if e.running {
		return
	}
	e.running = true
	log.Println("✅ 执行器已启动")
}
