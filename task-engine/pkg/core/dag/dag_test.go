package dag

import (
	"testing"

	"github.com/stevelan1995/task-engine/pkg/core/workflow"
)

func TestBuildDAG(t *testing.T) {
	tasks := map[string]workflow.Task{
		"task1": &mockTask{id: "task1", name: "task1"},
		"task2": &mockTask{id: "task2", name: "task2"},
	}

	dependencies := map[string][]string{
		"task2": {"task1"},
	}

	dag, err := BuildDAG(tasks, dependencies)
	if err != nil {
		t.Fatalf("构建DAG失败: %v", err)
	}

	if dag == nil {
		t.Fatal("DAG为nil")
	}

	if len(dag.Nodes) != 2 {
		t.Fatalf("节点数量错误，期望: 2, 实际: %d", len(dag.Nodes))
	}

	// 检查task2的入度
	task2Node := dag.Nodes["task2"]
	if task2Node.InDegree != 1 {
		t.Errorf("task2入度错误，期望: 1, 实际: %d", task2Node.InDegree)
	}

	// 检查task1的出边
	task1Node := dag.Nodes["task1"]
	if len(task1Node.OutEdges) != 1 || task1Node.OutEdges[0] != "task2" {
		t.Errorf("task1出边错误，期望: [task2], 实际: %v", task1Node.OutEdges)
	}
}

func TestDetectCycle_NoCycle(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", OutEdges: []string{"task2"}}
	dag.Nodes["task2"] = &Node{ID: "task2", OutEdges: []string{"task3"}}
	dag.Nodes["task3"] = &Node{ID: "task3", OutEdges: []string{}}

	err := dag.DetectCycle()
	if err != nil {
		t.Fatalf("无环DAG应该通过检测，但返回错误: %v", err)
	}
}

func TestDetectCycle_HasCycle(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", OutEdges: []string{"task2"}}
	dag.Nodes["task2"] = &Node{ID: "task2", OutEdges: []string{"task1"}}

	err := dag.DetectCycle()
	if err == nil {
		t.Fatal("有环DAG应该检测出错误，但未返回错误")
	}
}

func TestTopologicalSort(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", Name: "task1", InDegree: 0, OutEdges: []string{"task2", "task3"}}
	dag.Nodes["task2"] = &Node{ID: "task2", Name: "task2", InDegree: 1, OutEdges: []string{"task4"}}
	dag.Nodes["task3"] = &Node{ID: "task3", Name: "task3", InDegree: 1, OutEdges: []string{"task4"}}
	dag.Nodes["task4"] = &Node{ID: "task4", Name: "task4", InDegree: 2, OutEdges: []string{}}

	result, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("拓扑排序失败: %v", err)
	}

	if len(result.Levels) == 0 {
		t.Fatal("拓扑排序结果为空")
	}

	// 第一层应该只有task1
	if len(result.Levels[0]) != 1 || result.Levels[0][0] != "task1" {
		t.Errorf("第一层错误，期望: [task1], 实际: %v", result.Levels[0])
	}

	// 第二层应该有task2和task3
	if len(result.Levels) < 2 {
		t.Fatal("应该有至少2层")
	}
	if len(result.Levels[1]) != 2 {
		t.Errorf("第二层应该有两个节点，实际: %d", len(result.Levels[1]))
	}

	// 第三层应该有task4
	if len(result.Levels) < 3 {
		t.Fatal("应该有至少3层")
	}
	if len(result.Levels[2]) != 1 || result.Levels[2][0] != "task4" {
		t.Errorf("第三层错误，期望: [task4], 实际: %v", result.Levels[2])
	}
}

func TestAddNode(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", Name: "task1", InDegree: 0, OutEdges: []string{}}

	err := dag.AddNode("task2", "task2", []string{"task1"})
	if err != nil {
		t.Fatalf("添加节点失败: %v", err)
	}

	if _, exists := dag.Nodes["task2"]; !exists {
		t.Fatal("节点task2未添加")
	}

	task2Node := dag.Nodes["task2"]
	if task2Node.InDegree != 1 {
		t.Errorf("task2入度错误，期望: 1, 实际: %d", task2Node.InDegree)
	}

	task1Node := dag.Nodes["task1"]
	if len(task1Node.OutEdges) != 1 || task1Node.OutEdges[0] != "task2" {
		t.Errorf("task1出边错误，期望: [task2], 实际: %v", task1Node.OutEdges)
	}
}

func TestGetReadyTasks(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", InDegree: 0}
	dag.Nodes["task2"] = &Node{ID: "task2", InDegree: 1}
	dag.Nodes["task3"] = &Node{ID: "task3", InDegree: 0}

	ready := dag.GetReadyTasks()
	if len(ready) != 2 {
		t.Fatalf("就绪任务数量错误，期望: 2, 实际: %d", len(ready))
	}
}

func TestUpdateInDegree(t *testing.T) {
	dag := NewDAG()
	dag.Nodes["task1"] = &Node{ID: "task1", OutEdges: []string{"task2", "task3"}}
	dag.Nodes["task2"] = &Node{ID: "task2", InDegree: 1}
	dag.Nodes["task3"] = &Node{ID: "task3", InDegree: 1}

	dag.UpdateInDegree("task1")

	if dag.Nodes["task2"].InDegree != 0 {
		t.Errorf("task2入度错误，期望: 0, 实际: %d", dag.Nodes["task2"].InDegree)
	}

	if dag.Nodes["task3"].InDegree != 0 {
		t.Errorf("task3入度错误，期望: 0, 实际: %d", dag.Nodes["task3"].InDegree)
	}
}

// mockTask 用于测试的Task实现
type mockTask struct {
	id   string
	name string
}

func (m *mockTask) GetID() string {
	return m.id
}

func (m *mockTask) GetName() string {
	return m.name
}

func (m *mockTask) GetJobFuncName() string {
	return ""
}

func (m *mockTask) GetParams() map[string]interface{} {
	return nil
}

func (m *mockTask) GetStatus() string {
	return ""
}

func (m *mockTask) GetDependencies() []string {
	return nil
}
