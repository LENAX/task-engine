package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/begmaroman/go-dag"
	"github.com/google/uuid"
)

// -------------------------- 自定义节点（实现 go-dag 的 Identifiable 接口） --------------------------
// CustomNode 自定义DAG节点，包含生成新节点的字段
type CustomNode struct {
	NodeId      string // 节点唯一ID
	ParentId    string // 父节点ID（用于新生成的节点）
	Visited     bool   // 是否已访问
	GenerateNew int    // 生成新节点的数量
}

// ID 实现 Identifiable 接口
func (n *CustomNode) ID() string {
	return n.NodeId
}

// -------------------------- 三个核心协程逻辑 --------------------------

// traverseDAG 协程1：从根节点开始遍历DAG，找到所有可访问的节点（所有父节点都已完成的节点），发送到c1
// 如果没有可访问的节点，则关闭c1并退出
// 优化：使用候选节点队列，避免每次遍历整个图
// 注意：go-dag 库本身是线程安全的，无需额外加锁
func traverseDAG(d *dag.DAG[*CustomNode], c1 chan<- *CustomNode, wg *sync.WaitGroup, processedNodes *sync.Map, candidateNodes *sync.Map) {
	defer func() {
		wg.Done()
		close(c1)
		fmt.Println("[协程1] 没有可访问的节点，关闭c1并退出")
	}()

	fmt.Println("[协程1] 开始遍历DAG")

	for {
		// 获取可访问的节点（库内部已线程安全）
		availableNodes := findAvailableNodes(d, processedNodes, candidateNodes)

		if len(availableNodes) == 0 {
			// 没有可访问的节点，退出
			fmt.Println("[协程1] 没有可访问的节点，退出循环")
			return
		}

		// 发送所有可访问的节点到c1
		for _, node := range availableNodes {
			// 标记为已处理
			processedNodes.Store(node.NodeId, true)
			// 从候选队列中移除
			candidateNodes.Delete(node.NodeId)

			// 获取该节点的子节点，加入候选队列（库内部已线程安全）
			children, err := d.GetChildren(node.NodeId)
			if err == nil {
				for childID := range children {
					// 如果子节点还未处理，加入候选队列
					if _, processed := processedNodes.Load(childID); !processed {
						if child, err := d.GetVertex(childID); err == nil {
							candidateNodes.Store(childID, child)
						}
					}
				}
			}

			c1 <- node
			fmt.Printf("[协程1] 发送可访问节点 %s 到c1\n", node.NodeId)
		}

		// 短暂休眠，避免CPU占用过高
		time.Sleep(10 * time.Millisecond)
	}
}

// findAvailableNodes 找到所有可访问的节点（所有父节点都已被处理）
// 优化：只检查候选节点队列，而不是遍历整个图
func findAvailableNodes(d *dag.DAG[*CustomNode], processedNodes *sync.Map, candidateNodes *sync.Map) []*CustomNode {
	var available []*CustomNode

	// 只检查候选节点队列中的节点，而不是遍历整个图
	candidateNodes.Range(func(nodeID, nodeValue interface{}) bool {
		nodeIDStr := nodeID.(string)
		node := nodeValue.(*CustomNode)

		// 如果已经处理过，跳过
		if _, processed := processedNodes.Load(nodeIDStr); processed {
			return true // 继续遍历
		}

		// 获取该节点的所有父节点
		parents, err := d.GetParents(nodeIDStr)
		if err != nil {
			return true // 继续遍历
		}

		// 如果没有父节点，或者所有父节点都已处理，则该节点可访问
		allParentsProcessed := true
		if len(parents) > 0 {
			for parentID := range parents {
				if _, processed := processedNodes.Load(parentID); !processed {
					allParentsProcessed = false
					break
				}
			}
		}

		if allParentsProcessed {
			available = append(available, node)
		}

		return true // 继续遍历
	})

	return available
}

// processC1 协程2：监听c1，打印获取到的node
// 如果node的GenerateNew字段大于0，则生成等于该字段数量的node
// 将这些新node的ParentId设置成获取到的node的id，并将这些node发送到c2
// 如果c1关闭，则关闭c2并退出
func processC1(c1 <-chan *CustomNode, c2 chan<- *CustomNode, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
		close(c2)
		fmt.Println("[协程2] c1已关闭，关闭c2并退出")
	}()

	fmt.Println("[协程2] 开始监听c1通道")

	// 循环接收c1的节点（通道关闭时自动退出）
	for node := range c1 {
		// 打印接收到的节点信息
		fmt.Printf("[协程2] 从c1接收节点：NodeId=%s, GenerateNew=%d\n", node.NodeId, node.GenerateNew)

		// 如果GenerateNew > 0，生成新节点
		if node.GenerateNew > 0 {
			for i := 0; i < node.GenerateNew; i++ {
				newNode := &CustomNode{
					NodeId:      generateUniqueID(),
					ParentId:    node.NodeId,  // 设置父节点ID
					GenerateNew: rand.Intn(3), // 随机生成0-2个新节点
				}
				c2 <- newNode
				fmt.Printf("[协程2] 生成新节点 %s (ParentId=%s, GenerateNew=%d) 并发送到c2\n",
					newNode.NodeId, newNode.ParentId, newNode.GenerateNew)
			}
		}
	}
}

// processC2 协程3：监听c2，将获取到的node加入到dag中
// 如果c2被关闭则退出
// 注意：go-dag 库本身是线程安全的，无需额外加锁
func processC2(d *dag.DAG[*CustomNode], c2 <-chan *CustomNode, wg *sync.WaitGroup, candidateNodes *sync.Map, processedNodes *sync.Map) {
	defer func() {
		wg.Done()
		fmt.Println("[协程3] c2已关闭，退出")
	}()

	fmt.Println("[协程3] 开始监听c2通道")

	// 循环接收c2的节点（通道关闭时自动退出）
	for node := range c2 {
		// 将新节点添加到DAG中（库内部已线程安全）
		_, err := d.AddVertex(node)
		if err != nil {
			fmt.Printf("[协程3] 添加节点 %s 失败：%v\n", node.NodeId, err)
			continue
		}
		fmt.Printf("[协程3] 新节点 %s 已加入DAG\n", node.NodeId)

		// 如果新节点有父节点，添加边
		if node.ParentId != "" {
			// 检查父节点是否存在
			_, err := d.GetVertex(node.ParentId)
			if err == nil {
				// 添加边：ParentId -> NodeId
				if err := d.AddEdge(node.ParentId, node.NodeId); err != nil {
					fmt.Printf("[协程3] 添加边 %s -> %s 失败：%v\n", node.ParentId, node.NodeId, err)
				} else {
					fmt.Printf("[协程3] 添加边 %s -> %s\n", node.ParentId, node.NodeId)
				}
			} else {
				fmt.Printf("[协程3] 父节点 %s 不存在，跳过添加边\n", node.ParentId)
			}
		}

		// 检查新节点是否应该加入候选队列
		// 如果没有父节点（根节点），或者所有父节点都已处理，则加入候选队列
		parents, err := d.GetParents(node.NodeId)
		if err == nil {
			allParentsProcessed := true
			if len(parents) > 0 {
				for parentID := range parents {
					if _, processed := processedNodes.Load(parentID); !processed {
						allParentsProcessed = false
						break
					}
				}
			}
			if allParentsProcessed {
				// 加入候选队列
				candidateNodes.Store(node.NodeId, node)
			}
		}
	}
}

// -------------------------- 辅助函数 --------------------------

// generateUniqueID 生成唯一ID
func generateUniqueID() string {
	return fmt.Sprintf("node_%s", uuid.New().String()[:8])
}

// generateRandomDAG 生成随机DAG，确保有且只有一个根节点
func generateRandomDAG(d *dag.DAG[*CustomNode], nodeCount int) error {
	if nodeCount < 1 {
		return fmt.Errorf("节点数量必须至少为1")
	}

	// 创建节点
	nodes := make([]*CustomNode, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = &CustomNode{
			NodeId:      fmt.Sprintf("node_%d", i),
			GenerateNew: rand.Intn(3), // 随机生成0-2个新节点
			// GenerateNew: 0, // 随机生成0-2个新节点.
		}
		// 添加节点到DAG
		if _, err := d.AddVertex(nodes[i]); err != nil {
			return fmt.Errorf("添加节点 %s 失败: %v", nodes[i].NodeId, err)
		}
	}

	// 确保只有一个根节点：node_0 是唯一的根节点
	// 对于其他节点（node_1, node_2, ...），确保每个节点至少有一条入边
	for i := 1; i < nodeCount; i++ {
		// 从 node_0 到 node_{i-1} 中随机选择一个节点作为 node_i 的父节点
		// 确保 node_i 至少有一条入边
		parentIndex := rand.Intn(i) // 从 [0, i) 中随机选择
		if err := d.AddEdge(nodes[parentIndex].NodeId, nodes[i].NodeId); err != nil {
			return fmt.Errorf("添加边 %s -> %s 失败: %v", nodes[parentIndex].NodeId, nodes[i].NodeId, err)
		}
	}

	// 随机添加额外的边（可选，增加图的复杂度）
	// 只允许从编号小的节点指向编号大的节点，确保无环
	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			// 如果边已存在，跳过
			if isEdge, _ := d.IsEdge(nodes[i].NodeId, nodes[j].NodeId); isEdge {
				continue
			}
			// 30%的概率添加额外的边
			if rand.Float32() < 0.3 {
				if err := d.AddEdge(nodes[i].NodeId, nodes[j].NodeId); err != nil {
					// 忽略错误，继续添加其他边
					continue
				}
			}
		}
	}

	return nil
}

// -------------------------- 主线程逻辑 --------------------------

func main() {
	// 1. 初始化go-dag的DAG实例（库本身是线程安全的）
	d := dag.NewDAG[*CustomNode]()
	// 用于跟踪已处理的节点
	processedNodes := &sync.Map{}
	// 候选节点队列（优化：避免每次遍历整个图）
	candidateNodes := &sync.Map{}

	// 2. 生成初始的随机DAG，可以控制生成节点数量
	nodeCount := 5 // 可以调整这个值来控制初始节点数量
	fmt.Printf("=== 生成初始随机DAG，节点数量：%d ===\n", nodeCount)

	if err := generateRandomDAG(d, nodeCount); err != nil {
		fmt.Printf("生成随机DAG失败：%v\n", err)
		return
	}

	// 打印初始DAG信息并初始化候选节点队列
	roots := d.GetRoots()
	vertices := d.GetVertices()
	fmt.Printf("初始DAG：总节点数=%d, 根节点数=%d\n", len(vertices), len(roots))

	// 验证只有一个根节点
	if len(roots) != 1 {
		fmt.Printf("警告：期望只有1个根节点，但实际有 %d 个根节点\n", len(roots))
	}

	fmt.Println("根节点：")
	for rootID := range roots {
		fmt.Printf("  - %s\n", rootID)
		// 将根节点加入候选队列（根节点可以直接处理）
		if root, err := d.GetVertex(rootID); err == nil {
			candidateNodes.Store(rootID, root)
		}
	}

	// 3. 创建通道（带缓冲避免阻塞）
	c1 := make(chan *CustomNode, 10)
	c2 := make(chan *CustomNode, 10)

	// 4. 初始化等待组，等待3个协程完成
	var wg sync.WaitGroup
	wg.Add(3)

	// 5. 启动三个协程
	go traverseDAG(d, c1, &wg, processedNodes, candidateNodes)
	go processC1(c1, c2, &wg)
	go processC2(d, c2, &wg, candidateNodes, processedNodes)

	// 6. 等待所有协程执行完成
	wg.Wait()

	// 打印最终DAG信息
	finalVertices := d.GetVertices()
	fmt.Printf("\n=== 最终DAG：总节点数=%d ===\n", len(finalVertices))

	fmt.Println("\n=== 所有协程执行完成，主线程退出 ===")
}
