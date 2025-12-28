package dag

import (
	"fmt"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// BuildDAG 从Workflow构建DAG（对外导出）
// tasks: Task ID -> Task接口的映射
// dependencies: 后置Task ID -> 前置Task ID列表的映射
func BuildDAG(tasks map[string]workflow.Task, dependencies map[string][]string) (*DAG, error) {
	dag := NewDAG()

	// 1. 创建所有节点
	for taskID, task := range tasks {
		node := &Node{
			ID:       taskID,
			Name:     task.GetName(),
			InDegree: 0,
			OutEdges: make([]string, 0),
		}
		dag.Nodes[taskID] = node
	}

	// 2. 构建边和计算入度
	for taskID, depIDs := range dependencies {
		node, exists := dag.Nodes[taskID]
		if !exists {
			return nil, fmt.Errorf("Task ID %s 不存在", taskID)
		}

		// 设置入度
		node.InDegree = len(depIDs)

		// 为每个前置Task添加出边
		for _, depID := range depIDs {
			depNode, exists := dag.Nodes[depID]
			if !exists {
				return nil, fmt.Errorf("依赖的Task ID %s 不存在", depID)
			}
			depNode.OutEdges = append(depNode.OutEdges, taskID)
		}
	}

	return dag, nil
}

// DetectCycle 检测DAG中是否存在循环依赖（对外导出）
// 使用DFS算法检测环
func (d *DAG) DetectCycle() error {
	// 使用三色标记法：0=白色（未访问），1=灰色（正在访问），2=黑色（已访问）
	color := make(map[string]int)
	for id := range d.Nodes {
		color[id] = 0 // 白色
	}

	// 对每个节点进行DFS
	for id := range d.Nodes {
		if color[id] == 0 {
			if err := d.dfs(id, color); err != nil {
				return err
			}
		}
	}

	return nil
}

// dfs 深度优先搜索，检测环
func (d *DAG) dfs(nodeID string, color map[string]int) error {
	color[nodeID] = 1 // 灰色：正在访问

	node := d.Nodes[nodeID]
	for _, nextID := range node.OutEdges {
		if color[nextID] == 1 {
			// 发现后向边，存在环
			return fmt.Errorf("检测到循环依赖：%s -> %s", nodeID, nextID)
		}
		if color[nextID] == 0 {
			// 递归访问
			if err := d.dfs(nextID, color); err != nil {
				return err
			}
		}
	}

	color[nodeID] = 2 // 黑色：已访问完成
	return nil
}

// TopologicalSort 执行拓扑排序（对外导出）
// 返回拓扑排序结果，每一层的Task可以并行执行
func (d *DAG) TopologicalSort() (*TopologicalOrder, error) {
	// 先检测循环依赖
	if err := d.DetectCycle(); err != nil {
		return nil, fmt.Errorf("存在循环依赖，无法进行拓扑排序: %w", err)
	}

	// 复制入度（避免修改原始DAG）
	inDegreeCopy := make(map[string]int)
	for id, node := range d.Nodes {
		inDegreeCopy[id] = node.InDegree
	}

	result := &TopologicalOrder{
		Levels: make([][]string, 0),
	}

	// Kahn算法：不断找出入度为0的节点
	for {
		currentLevel := make([]string, 0)

		// 找出所有入度为0的节点
		for id, degree := range inDegreeCopy {
			if degree == 0 {
				currentLevel = append(currentLevel, id)
			}
		}

		// 如果没有入度为0的节点，说明所有节点都已处理或存在环
		if len(currentLevel) == 0 {
			// 检查是否还有未处理的节点
			for _, degree := range inDegreeCopy {
				if degree > 0 {
					return nil, fmt.Errorf("拓扑排序失败：存在未处理的节点（可能存在环）")
				}
			}
			break
		}

		// 将当前层的节点加入结果
		result.Levels = append(result.Levels, currentLevel)

		// 移除当前层节点，并更新下游节点的入度
		for _, nodeID := range currentLevel {
			delete(inDegreeCopy, nodeID)

			node := d.Nodes[nodeID]
			for _, nextID := range node.OutEdges {
				if _, exists := inDegreeCopy[nextID]; exists {
					inDegreeCopy[nextID]--
				}
			}
		}
	}

	return result, nil
}

// AddNode 动态添加节点到DAG（对外导出）
// 用于运行时添加子Task
func (d *DAG) AddNode(nodeID, nodeName string, parentIDs []string) error {
	// 检查节点是否已存在
	if _, exists := d.Nodes[nodeID]; exists {
		return fmt.Errorf("节点 %s 已存在", nodeID)
	}

	// 创建新节点
	node := &Node{
		ID:       nodeID,
		Name:     nodeName,
		InDegree: len(parentIDs),
		OutEdges: make([]string, 0),
	}
	d.Nodes[nodeID] = node

	// 更新父节点的出边
	for _, parentID := range parentIDs {
		parentNode, exists := d.Nodes[parentID]
		if !exists {
			return fmt.Errorf("父节点 %s 不存在", parentID)
		}
		parentNode.OutEdges = append(parentNode.OutEdges, nodeID)
	}

	return nil
}

// GetReadyTasks 获取当前就绪的Task ID列表（入度为0的节点）（对外导出）
func (d *DAG) GetReadyTasks() []string {
	ready := make([]string, 0)
	for id, node := range d.Nodes {
		if node.InDegree == 0 {
			ready = append(ready, id)
		}
	}
	return ready
}

// UpdateInDegree 更新节点的入度（对外导出）
// 当某个Task完成后，调用此方法更新其下游节点的入度
func (d *DAG) UpdateInDegree(completedTaskID string) {
	node, exists := d.Nodes[completedTaskID]
	if !exists {
		return
	}

	// 减少所有下游节点的入度
	for _, nextID := range node.OutEdges {
		nextNode, exists := d.Nodes[nextID]
		if exists && nextNode.InDegree > 0 {
			nextNode.InDegree--
		}
	}
}

