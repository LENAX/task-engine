package integration

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/engine"
	"github.com/stevelan1995/task-engine/pkg/plugin"
)

// testPlugin 测试插件
type testPlugin struct {
	name            string
	triggeredEvents []plugin.TriggerEvent
	triggeredData   []plugin.PluginData
	mu              sync.Mutex
}

func newTestPlugin(name string) *testPlugin {
	return &testPlugin{
		name:            name,
		triggeredEvents: make([]plugin.TriggerEvent, 0),
		triggeredData:   make([]plugin.PluginData, 0),
	}
}

func (t *testPlugin) Name() string {
	return t.name
}

func (t *testPlugin) Init(params map[string]string) error {
	return nil
}

func (t *testPlugin) Execute(data interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	pluginData := data.(plugin.PluginData)
	t.triggeredEvents = append(t.triggeredEvents, pluginData.Event)
	t.triggeredData = append(t.triggeredData, pluginData)
	return nil
}

func (t *testPlugin) GetTriggeredEvents() []plugin.TriggerEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]plugin.TriggerEvent, len(t.triggeredEvents))
	copy(result, t.triggeredEvents)
	return result
}

func (t *testPlugin) GetTriggeredData() []plugin.PluginData {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]plugin.PluginData, len(t.triggeredData))
	copy(result, t.triggeredData)
	return result
}

// createPluginTestConfig 创建插件测试配置
func createPluginTestConfig(t *testing.T) string {
	configContent := `
database:
  type: "sqlite"
  dsn: "file::memory:?cache=shared&_journal_mode=WAL"

worker:
  concurrency: 10
  default_task_timeout: "30s"
`
	configFile := "test_config_plugin.yaml"
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	return configFile
}

// TestPlugin_WorkflowEvents 测试Workflow事件的插件触发
func TestPlugin_WorkflowEvents(t *testing.T) {
	configFile := createPluginTestConfig(t)
	defer os.Remove(configFile)

	// 创建测试插件
	testPlugin := newTestPlugin("test_plugin")

	// 创建Engine并注册插件
	engineBuilder := engine.NewEngineBuilder(configFile).
		WithPlugin(testPlugin).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventWorkflowStarted,
		}).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventWorkflowCompleted,
		}).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventWorkflowFailed,
		})

	eng, err := engineBuilder.Build()
	if err != nil {
		t.Fatalf("构建Engine失败: %v", err)
	}

	// 启动Engine
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 注册Job函数
	jobFunc := func(ctx context.Context) error {
		return nil
	}
	_, err = eng.GetRegistry().Register(ctx, "test_job", jobFunc, "测试Job")
	if err != nil {
		t.Fatalf("注册Job函数失败: %v", err)
	}

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("test_workflow", "测试Workflow")
	taskBuilder := builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry())
	task1, err := taskBuilder.WithJobFunction("test_job", nil).Build()
	if err != nil {
		t.Fatalf("创建Task失败: %v", err)
	}
	wf, err := wfBuilder.WithTask(task1).Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	// 提交Workflow
	instanceID, err := eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待Workflow完成
	time.Sleep(2 * time.Second)

	// 检查插件是否被触发
	events := testPlugin.GetTriggeredEvents()
	if len(events) < 2 {
		t.Fatalf("插件应该至少被触发2次（Started和Completed），实际: %d", len(events))
	}

	// 验证WorkflowStarted事件
	hasStarted := false
	for _, event := range events {
		if event == plugin.EventWorkflowStarted {
			hasStarted = true
			break
		}
	}
	if !hasStarted {
		t.Fatal("插件应该被WorkflowStarted事件触发")
	}

	// 验证WorkflowCompleted事件
	hasCompleted := false
	for _, event := range events {
		if event == plugin.EventWorkflowCompleted {
			hasCompleted = true
			break
		}
	}
	if !hasCompleted {
		t.Fatal("插件应该被WorkflowCompleted事件触发")
	}

	// 验证插件数据
	data := testPlugin.GetTriggeredData()
	if len(data) < 2 {
		t.Fatalf("插件数据应该至少有2条，实际: %d", len(data))
	}
	for _, d := range data {
		if d.InstanceID != instanceID.InstanceID() {
			t.Fatalf("插件数据中的InstanceID不正确: %s", d.InstanceID)
		}
	}
}

// TestPlugin_TaskEvents 测试Task事件的插件触发
func TestPlugin_TaskEvents(t *testing.T) {
	configFile := createPluginTestConfig(t)
	defer os.Remove(configFile)

	// 创建测试插件
	testPlugin := newTestPlugin("test_plugin")

	// 创建Engine并注册插件
	engineBuilder := engine.NewEngineBuilder(configFile).
		WithPlugin(testPlugin).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventTaskSuccess,
		}).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventTaskFailed,
		})

	eng, err := engineBuilder.Build()
	if err != nil {
		t.Fatalf("构建Engine失败: %v", err)
	}

	// 启动Engine
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 注册Job函数
	jobFunc := func(ctx context.Context) error {
		return nil
	}
	_, err = eng.GetRegistry().Register(ctx, "test_job", jobFunc, "测试Job")
	if err != nil {
		t.Fatalf("注册Job函数失败: %v", err)
	}

	// 创建Workflow
	wfBuilder := builder.NewWorkflowBuilder("test_workflow", "测试Workflow")
	taskBuilder := builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry())
	task1, err := taskBuilder.WithJobFunction("test_job", nil).Build()
	if err != nil {
		t.Fatalf("创建Task失败: %v", err)
	}
	wf, err := wfBuilder.WithTask(task1).Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	// 提交Workflow
	_, err = eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待Task完成
	time.Sleep(2 * time.Second)

	// 检查插件是否被触发
	events := testPlugin.GetTriggeredEvents()
	hasTaskSuccess := false
	for _, event := range events {
		if event == plugin.EventTaskSuccess {
			hasTaskSuccess = true
			break
		}
	}
	if !hasTaskSuccess {
		t.Fatal("插件应该被TaskSuccess事件触发")
	}

	// 验证插件数据
	data := testPlugin.GetTriggeredData()
	hasTaskData := false
	for _, d := range data {
		if d.Event == plugin.EventTaskSuccess && d.TaskID == "task1" {
			hasTaskData = true
			break
		}
	}
	if !hasTaskData {
		t.Fatal("插件数据应该包含Task信息")
	}
}

// TestPlugin_BuilderConfiguration 测试通过Builder配置插件
func TestPlugin_BuilderConfiguration(t *testing.T) {
	configFile := createPluginTestConfig(t)
	defer os.Remove(configFile)

	// 创建测试插件
	testPlugin := newTestPlugin("test_plugin")

	// 通过Builder注册插件和绑定
	engineBuilder := engine.NewEngineBuilder(configFile).
		WithPlugin(testPlugin).
		WithPluginBinding(plugin.PluginBinding{
			PluginName: "test_plugin",
			Event:      plugin.EventWorkflowStarted,
		})

	eng, err := engineBuilder.Build()
	if err != nil {
		t.Fatalf("构建Engine失败: %v", err)
	}

	// 验证插件已注册
	pm := eng.GetPluginManager()
	_, exists := pm.GetPlugin("test_plugin")
	if !exists {
		t.Fatal("插件应该已注册")
	}

	// 启动Engine
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("启动Engine失败: %v", err)
	}
	defer eng.Stop()

	// 注册Job函数
	jobFunc := func(ctx context.Context) error {
		return nil
	}
	_, err = eng.GetRegistry().Register(ctx, "test_job", jobFunc, "测试Job")
	if err != nil {
		t.Fatalf("注册Job函数失败: %v", err)
	}

	// 创建并提交Workflow
	task1, err := builder.NewTaskBuilder("task1", "任务1", eng.GetRegistry()).
		WithJobFunction("test_job", nil).
		Build()
	if err != nil {
		t.Fatalf("创建Task失败: %v", err)
	}
	wf, err := builder.NewWorkflowBuilder("test_workflow", "测试Workflow").
		WithTask(task1).
		Build()
	if err != nil {
		t.Fatalf("创建Workflow失败: %v", err)
	}

	_, err = eng.SubmitWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("提交Workflow失败: %v", err)
	}

	// 等待Workflow启动
	time.Sleep(1 * time.Second)

	// 验证插件被触发
	events := testPlugin.GetTriggeredEvents()
	hasStarted := false
	for _, event := range events {
		if event == plugin.EventWorkflowStarted {
			hasStarted = true
			break
		}
	}
	if !hasStarted {
		t.Fatal("插件应该被WorkflowStarted事件触发")
	}
}
