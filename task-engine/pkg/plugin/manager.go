package plugin

import (
	"context"
	"fmt"
	"sync"
)

// TriggerEvent 插件触发事件类型（对外导出）
type TriggerEvent string

const (
	// Workflow事件
	EventWorkflowStarted    TriggerEvent = "workflow.started"    // Workflow启动
	EventWorkflowCompleted  TriggerEvent = "workflow.completed"  // Workflow完成
	EventWorkflowFailed     TriggerEvent = "workflow.failed"     // Workflow失败
	EventWorkflowPaused     TriggerEvent = "workflow.paused"     // Workflow暂停
	EventWorkflowResumed    TriggerEvent = "workflow.resumed"    // Workflow恢复
	EventWorkflowTerminated TriggerEvent = "workflow.terminated" // Workflow终止

	// Task事件
	EventTaskStarted  TriggerEvent = "task.started"  // Task开始执行
	EventTaskSuccess  TriggerEvent = "task.success"  // Task执行成功
	EventTaskFailed   TriggerEvent = "task.failed"   // Task执行失败
	EventTaskTimeout  TriggerEvent = "task.timeout"  // Task执行超时
	EventTaskRetrying TriggerEvent = "task.retrying" // Task重试中
)

// PluginBinding 插件绑定规则（对外导出）
type PluginBinding struct {
	PluginName string              // 插件名称
	Event      TriggerEvent        // 触发事件
	Params     map[string]string   // 插件初始化参数
	Condition  func(data any) bool // 可选：条件函数，满足条件才触发
}

// PluginData 传递给插件的数据（对外导出）
type PluginData struct {
	Event      TriggerEvent           // 触发事件
	WorkflowID string                 // Workflow ID
	InstanceID string                 // WorkflowInstance ID（如果有）
	TaskID     string                 // Task ID（如果有）
	TaskName   string                 // Task名称（如果有）
	Status     string                 // 状态
	Error      error                  // 错误信息（如果有）
	Data       map[string]interface{} // 自定义数据
}

// PluginManager 插件管理器接口（对外导出）
type PluginManager interface {
	// Register 注册插件
	Register(plugin Plugin) error
	// RegisterWithInit 注册并初始化插件
	RegisterWithInit(plugin Plugin, params map[string]string) error
	// Bind 绑定插件到事件
	Bind(binding PluginBinding) error
	// Trigger 触发插件
	Trigger(ctx context.Context, event TriggerEvent, data PluginData) error
	// GetPlugin 获取已注册的插件
	GetPlugin(name string) (Plugin, bool)
	// ListPlugins 列出所有已注册的插件
	ListPlugins() []string
	// Unregister 取消注册插件
	Unregister(name string) error
}

// pluginManagerImpl 插件管理器实现（内部实现）
type pluginManagerImpl struct {
	plugins  map[string]Plugin                // 已注册的插件（插件名称 -> 插件实例）
	bindings map[TriggerEvent][]PluginBinding // 事件绑定（事件类型 -> 绑定列表）
	mu       sync.RWMutex                     // 读写锁
}

// NewPluginManager 创建插件管理器（对外导出）
func NewPluginManager() PluginManager {
	return &pluginManagerImpl{
		plugins:  make(map[string]Plugin),
		bindings: make(map[TriggerEvent][]PluginBinding),
	}
}

// Register 注册插件（实现PluginManager接口）
func (pm *pluginManagerImpl) Register(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("插件不能为空")
	}

	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("插件名称不能为空")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("插件 %s 已注册", name)
	}

	pm.plugins[name] = plugin
	return nil
}

// RegisterWithInit 注册并初始化插件（实现PluginManager接口）
func (pm *pluginManagerImpl) RegisterWithInit(plugin Plugin, params map[string]string) error {
	if err := pm.Register(plugin); err != nil {
		return err
	}

	// 初始化插件
	if err := plugin.Init(params); err != nil {
		// 初始化失败，移除已注册的插件
		pm.mu.Lock()
		delete(pm.plugins, plugin.Name())
		pm.mu.Unlock()
		return fmt.Errorf("插件 %s 初始化失败: %w", plugin.Name(), err)
	}

	return nil
}

// Bind 绑定插件到事件（实现PluginManager接口）
func (pm *pluginManagerImpl) Bind(binding PluginBinding) error {
	if binding.PluginName == "" {
		return fmt.Errorf("插件名称不能为空")
	}

	if binding.Event == "" {
		return fmt.Errorf("触发事件不能为空")
	}

	pm.mu.RLock()
	_, exists := pm.plugins[binding.PluginName]
	pm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("插件 %s 未注册", binding.PluginName)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.bindings[binding.Event] = append(pm.bindings[binding.Event], binding)
	return nil
}

// Trigger 触发插件（实现PluginManager接口）
func (pm *pluginManagerImpl) Trigger(ctx context.Context, event TriggerEvent, data PluginData) error {
	pm.mu.RLock()
	bindings, exists := pm.bindings[event]
	pm.mu.RUnlock()

	if !exists || len(bindings) == 0 {
		return nil // 没有绑定，直接返回
	}

	var errors []error
	for _, binding := range bindings {
		// 检查条件
		if binding.Condition != nil && !binding.Condition(data) {
			continue
		}

		// 获取插件
		pm.mu.RLock()
		plugin, exists := pm.plugins[binding.PluginName]
		pm.mu.RUnlock()

		if !exists {
			continue // 插件不存在，跳过
		}

		// 执行插件
		if err := plugin.Execute(data); err != nil {
			errors = append(errors, fmt.Errorf("插件 %s 执行失败: %w", binding.PluginName, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("触发插件失败: %v", errors)
	}

	return nil
}

// GetPlugin 获取已注册的插件（实现PluginManager接口）
func (pm *pluginManagerImpl) GetPlugin(name string) (Plugin, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	plugin, exists := pm.plugins[name]
	return plugin, exists
}

// ListPlugins 列出所有已注册的插件（实现PluginManager接口）
func (pm *pluginManagerImpl) ListPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}
	return names
}

// Unregister 取消注册插件（实现PluginManager接口）
func (pm *pluginManagerImpl) Unregister(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; !exists {
		return fmt.Errorf("插件 %s 未注册", name)
	}

	delete(pm.plugins, name)

	// 移除所有相关的绑定
	for event := range pm.bindings {
		filtered := make([]PluginBinding, 0)
		for _, binding := range pm.bindings[event] {
			if binding.PluginName != name {
				filtered = append(filtered, binding)
			}
		}
		pm.bindings[event] = filtered
	}

	return nil
}
