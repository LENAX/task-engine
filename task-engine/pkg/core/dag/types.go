package dag

import (
	godag "github.com/begmaroman/go-dag"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

// dagImpl 有向无环图实现（内部实现）
// 封装 go-dag 库的 DAG，使用 workflow.Task 作为节点值
type dagImpl struct {
	*godag.DAG[workflow.Task] // 嵌入 go-dag 的 DAG
}

// NewDAG 创建新的DAG实例（对外导出）
func NewDAG() DAG {
	return &dagImpl{
		DAG: godag.NewDAG[workflow.Task](),
	}
}

// TopologicalOrder 拓扑排序结果（对外导出）
type TopologicalOrder struct {
	Levels [][]string // 每一层的Task ID列表，可以并行执行
}

// Node 节点信息（用于兼容旧接口，实际使用 go-dag 的内部结构）
type Node struct {
	ID       string   // 节点ID（Task ID）
	Name     string   // 节点名称（Task名称）
	InDegree int      // 入度（依赖的前置Task数量）
	OutEdges []string // 出边（下游依赖该节点的Task ID列表）
}
