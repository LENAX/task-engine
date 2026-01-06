package dag

import (
	"fmt"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// BuildDAGOptions DAG构建选项
type BuildDAGOptions struct {
	SkipCycleCheck bool // 跳过循环检测（适用于已知无环的场景，如层级依赖）
}

// BuildDAG 从Workflow构建DAG（对外导出）
// tasks: Task ID -> Task接口的映射
// dependencies: 后置Task ID -> 前置Task ID列表的映射
func BuildDAG(tasks map[string]workflow.Task, dependencies map[string][]string) (DAG, error) {
	return BuildDAGWithOptions(tasks, dependencies, BuildDAGOptions{})
}

// detectCycleDFS 使用DFS检测图中是否存在循环（高效的循环检测算法）
// graph: 邻接表，key是节点ID，value是该节点的所有子节点ID列表
func detectCycleDFS(graph map[string][]string) (bool, []string) {
	// 使用三色标记法：0=白色（未访问），1=灰色（正在访问），2=黑色（已访问）
	color := make(map[string]int)
	parent := make(map[string]string)
	cyclePath := make([]string, 0)

	// 初始化所有节点为白色
	for nodeID := range graph {
		color[nodeID] = 0
	}

	// DFS遍历所有节点
	var dfs func(nodeID string) bool
	dfs = func(nodeID string) bool {
		// 标记为灰色（正在访问）
		color[nodeID] = 1

		// 遍历所有子节点
		for _, childID := range graph[nodeID] {
			if color[childID] == 0 {
				// 白色节点，递归访问
				parent[childID] = nodeID
				if dfs(childID) {
					return true
				}
			} else if color[childID] == 1 {
				// 灰色节点，说明存在后向边，检测到循环
				// 构建循环路径
				cyclePath = append(cyclePath, childID)
				cur := nodeID
				for cur != childID && cur != "" {
					cyclePath = append(cyclePath, cur)
					cur = parent[cur]
				}
				cyclePath = append(cyclePath, childID) // 闭合循环
				return true
			}
			// 黑色节点，跳过（已访问且无循环）
		}

		// 标记为黑色（已访问）
		color[nodeID] = 2
		return false
	}

	// 对所有节点进行DFS
	for nodeID := range graph {
		if color[nodeID] == 0 {
			if dfs(nodeID) {
				return true, cyclePath
			}
		}
	}

	return false, nil
}

// BuildDAGWithOptions 从Workflow构建DAG（带选项）
// tasks: Task ID -> Task接口的映射
// dependencies: 后置Task ID -> 前置Task ID列表的映射
// options: 构建选项
func BuildDAGWithOptions(tasks map[string]workflow.Task, dependencies map[string][]string, options BuildDAGOptions) (DAG, error) {
	// 优化：先构建临时图结构，一次性检测循环，然后再添加到 go-dag 的 DAG 中
	// 这样可以避免在每次 AddEdge 时都进行递归检查，大大提高性能

	// 1. 构建临时图结构（邻接表）
	graph := make(map[string][]string)
	// 初始化所有节点（确保所有节点都在图中，即使没有边）
	for taskID := range tasks {
		graph[taskID] = make([]string, 0)
	}
	// 添加所有边到临时图
	for taskID, depIDs := range dependencies {
		// 确保 taskID 在图中
		if _, exists := graph[taskID]; !exists {
			graph[taskID] = make([]string, 0)
		}
		// 添加边：depID -> taskID（前置Task -> 后置Task）
		for _, depID := range depIDs {
			// 确保 depID 在图中
			if _, exists := graph[depID]; !exists {
				graph[depID] = make([]string, 0)
			}
			// 添加边到邻接表
			graph[depID] = append(graph[depID], taskID)
		}
	}

	// 2. 一次性检测循环（使用高效的DFS算法）
	if !options.SkipCycleCheck {
		hasCycle, cyclePath := detectCycleDFS(graph)
		if hasCycle {
			return nil, fmt.Errorf("检测到循环依赖: %v", cyclePath)
		}
	}

	// 3. 创建 go-dag 的 DAG 实例并添加所有节点
	d := NewDAG()
	dagImpl := d.(*dagImpl) // 类型断言，因为需要访问内部方法
	for taskID, task := range tasks {
		if err := dagImpl.AddVertexByID(taskID, task); err != nil {
			return nil, fmt.Errorf("添加节点失败: Task ID=%s, Error=%w", taskID, err)
		}
	}

	// 4. 一次性添加所有边（虽然 go-dag 库内部仍会检查，但我们已经确认无环，所以不会失败）
	edgeCount := 0
	for taskID, depIDs := range dependencies {
		for _, depID := range depIDs {
			edgeCount++
			// 添加边：depID -> taskID（前置Task -> 后置Task）
			if err := dagImpl.AddEdge(depID, taskID); err != nil {
				// 如果设置了跳过循环检测，但仍然检测到循环，说明确实存在循环
				if options.SkipCycleCheck {
					return nil, fmt.Errorf("添加边失败: %s -> %s, Error=%w (即使跳过循环检测，go-dag库仍会检测)", depID, taskID, err)
				}
				return nil, fmt.Errorf("添加边失败: %s -> %s, Error=%w", depID, taskID, err)
			}
		}
	}

	return d, nil
}

// DetectCycle 检测DAG中是否存在循环依赖（对外导出）
// go-dag 库在 AddEdge 时会自动检测循环，但这里提供一个显式的检测方法
func (d *dagImpl) DetectCycle() error {
	// go-dag 库在添加边时会自动检测循环依赖
	// 如果已经构建完成，可以通过尝试复制来检测（复制操作会检测循环）
	_, err := d.Copy()
	if err != nil {
		// 如果复制失败，可能是存在循环
		return fmt.Errorf("检测到循环依赖: %w", err)
	}
	return nil
}

// TopologicalSort 执行拓扑排序（对外导出）
// 返回拓扑排序结果，每一层的Task可以并行执行
func (d *dagImpl) TopologicalSort() (*TopologicalOrder, error) {
	// 先检测循环依赖
	if err := d.DetectCycle(); err != nil {
		return nil, fmt.Errorf("存在循环依赖，无法进行拓扑排序: %w", err)
	}

	result := &TopologicalOrder{
		Levels: make([][]string, 0),
	}

	// 使用 Kahn 算法进行拓扑排序
	// 1. 计算每个节点的入度
	inDegree := make(map[string]int)
	vertices := d.GetVertices()
	for id := range vertices {
		parents, _ := d.GetParents(id)
		inDegree[id] = len(parents)
	}

	// 2. 找出所有入度为0的节点（根节点）
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// 3. 不断移除入度为0的节点，并更新其子节点的入度
	for len(queue) > 0 {
		currentLevel := make([]string, 0)
		nextQueue := make([]string, 0)

		// 处理当前层的所有节点
		for _, nodeID := range queue {
			currentLevel = append(currentLevel, nodeID)

			// 获取子节点并减少其入度
			children, _ := d.GetChildren(nodeID)
			for _, childID := range children {
				inDegree[childID]--
				if inDegree[childID] == 0 {
					nextQueue = append(nextQueue, childID)
				}
			}
		}

		result.Levels = append(result.Levels, currentLevel)
		queue = nextQueue
	}

	// 4. 检查是否所有节点都被处理
	if len(result.Levels) == 0 || len(result.Levels[0]) == 0 {
		// 检查是否还有未处理的节点
		for _, degree := range inDegree {
			if degree > 0 {
				return nil, fmt.Errorf("拓扑排序失败：存在未处理的节点（可能存在环）")
			}
		}
	}

	return result, nil
}

// AddNode 动态添加节点到DAG（对外导出）
// 用于运行时添加子Task
// task: 要添加的 Task 实例
func (d *dagImpl) AddNode(nodeID, nodeName string, task workflow.Task, parentIDs []string) error {
	// 检查节点是否已存在
	if _, err := d.GetVertex(nodeID); err == nil {
		return fmt.Errorf("节点 %s 已存在", nodeID)
	}

	// 添加节点
	if err := d.AddVertexByID(nodeID, task); err != nil {
		return fmt.Errorf("添加节点失败: %w", err)
	}

	// 添加边（依赖关系）
	for _, parentID := range parentIDs {
		if err := d.AddEdge(parentID, nodeID); err != nil {
			return fmt.Errorf("添加边失败: %s -> %s, Error=%w", parentID, nodeID, err)
		}
	}

	return nil
}

// GetReadyTasks 获取当前就绪的Task ID列表（入度为0的节点，即根节点）（实现DAG接口）
func (d *dagImpl) GetReadyTasks() []string {
	roots := d.DAG.GetRoots()
	ready := make([]string, 0, len(roots))
	for id := range roots {
		ready = append(ready, id)
	}
	return ready
}

// UpdateInDegree 更新节点的入度（对外导出）
// 当某个Task完成后，调用此方法更新其下游节点的入度
// 注意：go-dag 库是只读的，不能直接修改入度
// 这个方法主要用于标记节点已完成，实际入度由 DAG 结构自动管理
func (d *dagImpl) UpdateInDegree(completedTaskID string) {
	// go-dag 库的 DAG 结构是只读的，入度由边的结构自动管理
	// 这个方法保留用于兼容性，但实际不需要做任何事情
	// 因为 go-dag 会自动管理入度
}

// GetChildren 获取节点的子节点（对外导出）
// 兼容旧接口
func (d *dagImpl) GetChildren(nodeID string) ([]string, error) {
	children, err := d.DAG.GetChildren(nodeID)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(children))
	for id := range children {
		result = append(result, id)
	}
	return result, nil
}

// GetParents 获取节点的父节点（对外导出）
// 兼容旧接口
func (d *dagImpl) GetParents(nodeID string) ([]string, error) {
	parents, err := d.DAG.GetParents(nodeID)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(parents))
	for id := range parents {
		result = append(result, id)
	}
	return result, nil
}

// GetVertices 获取所有节点（实现DAG接口）
func (d *dagImpl) GetVertices() map[string]workflow.Task {
	return d.DAG.GetVertices()
}

// GetRoots 获取所有根节点（实现DAG接口）
func (d *dagImpl) GetRoots() map[string]workflow.Task {
	roots := d.DAG.GetRoots()
	result := make(map[string]workflow.Task, len(roots))
	for id := range roots {
		if task, err := d.DAG.GetVertex(id); err == nil {
			result[id] = task
		}
	}
	return result
}

// GetVertex 获取指定节点（实现DAG接口）
func (d *dagImpl) GetVertex(nodeID string) (workflow.Task, error) {
	return d.DAG.GetVertex(nodeID)
}

// Nodes 获取所有节点（兼容旧接口）
// 返回节点ID到节点的映射
// 注意：为了兼容旧代码，保留此方法，但建议直接使用 go-dag 的方法
func (d *dagImpl) Nodes() map[string]*Node {
	vertices := d.GetVertices()
	nodes := make(map[string]*Node, len(vertices))

	for id, task := range vertices {
		// 获取父节点和子节点
		parents, _ := d.GetParents(id)
		children, _ := d.GetChildren(id)

		nodes[id] = &Node{
			ID:       id,
			Name:     task.GetName(),
			InDegree: len(parents),
			OutEdges: children,
		}
	}

	return nodes
}

// GetNode 获取指定节点（兼容旧接口）
func (d *dagImpl) GetNode(nodeID string) (*Node, bool) {
	task, err := d.GetVertex(nodeID)
	if err != nil {
		return nil, false
	}

	parents, _ := d.GetParents(nodeID)
	children, _ := d.GetChildren(nodeID)

	return &Node{
		ID:       nodeID,
		Name:     task.GetName(),
		InDegree: len(parents),
		OutEdges: children,
	}, true
}
