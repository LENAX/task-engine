package dag

import (
	"github.com/LENAX/task-engine/pkg/core/workflow"
)

// DAG 有向无环图接口（对外导出）
type DAG interface {
	// DetectCycle 检测DAG中是否存在循环依赖
	DetectCycle() error
	// TopologicalSort 执行拓扑排序
	TopologicalSort() (*TopologicalOrder, error)
	// AddNode 动态添加节点到DAG
	AddNode(nodeID, nodeName string, task workflow.Task, parentIDs []string) error
	// GetReadyTasks 获取当前就绪的Task ID列表（入度为0的节点）
	GetReadyTasks() []string
	// UpdateInDegree 更新节点的入度（兼容方法，实际由DAG结构自动管理）
	UpdateInDegree(completedTaskID string)
	// GetChildren 获取节点的子节点
	GetChildren(nodeID string) ([]string, error)
	// GetParents 获取节点的父节点
	GetParents(nodeID string) ([]string, error)
	// GetVertices 获取所有节点
	GetVertices() map[string]workflow.Task
	// GetRoots 获取所有根节点（入度为0的节点）
	GetRoots() map[string]workflow.Task
	// GetVertex 获取指定节点
	GetVertex(nodeID string) (workflow.Task, error)
	// GetNode 获取指定节点（兼容旧接口，返回Node结构）
	GetNode(nodeID string) (*Node, bool)
}

