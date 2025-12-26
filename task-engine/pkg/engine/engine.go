package engine

import (
    "github.com/stevelan1995/task-engine/pkg/config"
)

// Engine 调度引擎核心结构体
type Engine struct {
    cfg *config.Config
}

// NewEngine 创建引擎实例
func NewEngine(cfg *config.Config) (*Engine, error) {
    return &Engine{
        cfg: cfg,
    }, nil
}

// Stop 停止引擎
func (e *Engine) Stop() {
    // TODO: 实现引擎停止逻辑（关闭存储连接、停止定时任务等）
}
