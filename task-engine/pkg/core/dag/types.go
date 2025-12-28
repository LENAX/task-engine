package dag

// Node DAG节点结构（对外导出）
type Node struct {
	ID       string   // 节点ID（Task ID）
	Name     string   // 节点名称（Task名称）
	InDegree int      // 入度（依赖的前置Task数量）
	OutEdges []string // 出边（下游依赖该节点的Task ID列表）
}

// DAG 有向无环图结构（对外导出）
type DAG struct {
	Nodes map[string]*Node // 节点ID -> 节点
}

// NewDAG 创建新的DAG实例（对外导出）
func NewDAG() *DAG {
	return &DAG{
		Nodes: make(map[string]*Node),
	}
}

// TopologicalOrder 拓扑排序结果（对外导出）
type TopologicalOrder struct {
	Levels [][]string // 每一层的Task ID列表，可以并行执行
}

