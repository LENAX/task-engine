package engine

import "github.com/LENAX/task-engine/pkg/core/workflow"

// subTaskPool 管理单个模板任务下的子任务队列与计数。
// 该结构由 V3 actor 单线程访问，不做并发锁保护。
type subTaskPool struct {
	parentID     string
	tasks        []workflow.Task
	retryQueue   []workflow.Task
	cursor       int
	runningCount int
	completed    int
	failed       int
	total        int
	jobDone      bool
	allDone      bool
}

func newSubTaskPool(parentID string) *subTaskPool {
	return &subTaskPool{
		parentID:   parentID,
		tasks:      make([]workflow.Task, 0, 64),
		retryQueue: make([]workflow.Task, 0, 16),
	}
}

func (p *subTaskPool) addTasks(tasks []workflow.Task) {
	if len(tasks) == 0 {
		return
	}
	p.tasks = append(p.tasks, tasks...)
	p.total += len(tasks)
	p.recomputeAllDone()
}

func (p *subTaskPool) next(count int) []workflow.Task {
	if count <= 0 {
		return nil
	}
	result := make([]workflow.Task, 0, count)

	// 优先提交重试队列
	for len(p.retryQueue) > 0 && len(result) < count {
		lastIdx := len(p.retryQueue) - 1
		result = append(result, p.retryQueue[lastIdx])
		p.retryQueue = p.retryQueue[:lastIdx]
	}

	// 再提交普通队列
	remain := count - len(result)
	if remain > 0 && p.cursor < len(p.tasks) {
		end := p.cursor + remain
		if end > len(p.tasks) {
			end = len(p.tasks)
		}
		result = append(result, p.tasks[p.cursor:end]...)
		p.cursor = end
	}
	return result
}

func (p *subTaskPool) addRunning(n int) {
	if n <= 0 {
		return
	}
	p.runningCount += n
	p.recomputeAllDone()
}

func (p *subTaskPool) taskCompleted() {
	if p.runningCount > 0 {
		p.runningCount--
	}
	p.completed++
	p.recomputeAllDone()
}

func (p *subTaskPool) taskFailed() {
	if p.runningCount > 0 {
		p.runningCount--
	}
	p.failed++
	p.recomputeAllDone()
}

func (p *subTaskPool) enqueueRetry(task workflow.Task) {
	if task == nil {
		return
	}
	p.retryQueue = append(p.retryQueue, task)
	p.recomputeAllDone()
}

func (p *subTaskPool) hasReady() bool {
	return len(p.retryQueue) > 0 || p.cursor < len(p.tasks)
}

func (p *subTaskPool) markJobDone() {
	p.jobDone = true
	p.recomputeAllDone()
}

func (p *subTaskPool) recomputeAllDone() {
	processed := p.completed + p.failed
	p.allDone = p.jobDone && processed >= p.total && p.runningCount == 0 && !p.hasReady()
}
