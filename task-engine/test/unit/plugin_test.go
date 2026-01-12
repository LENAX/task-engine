package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/LENAX/task-engine/pkg/plugin"
)

// mockPlugin 测试用的Mock插件
type mockPlugin struct {
	name            string
	initParams      map[string]string
	executeData     interface{}
	initError       error
	executeError    error
	executeCallCount int
}

func newMockPlugin(name string) *mockPlugin {
	return &mockPlugin{
		name: name,
	}
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Init(params map[string]string) error {
	m.initParams = params
	return m.initError
}

func (m *mockPlugin) Execute(data interface{}) error {
	m.executeData = data
	m.executeCallCount++
	return m.executeError
}

// TestPluginManager_Register 测试插件注册
func TestPluginManager_Register(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 测试正常注册
	p1 := newMockPlugin("test_plugin")
	err := pm.Register(p1)
	if err != nil {
		t.Fatalf("注册插件失败: %v", err)
	}

	// 测试重复注册
	err = pm.Register(p1)
	if err == nil {
		t.Fatal("重复注册应该失败")
	}

	// 测试空插件
	err = pm.Register(nil)
	if err == nil {
		t.Fatal("空插件应该失败")
	}

	// 测试空名称插件
	p2 := newMockPlugin("")
	err = pm.Register(p2)
	if err == nil {
		t.Fatal("空名称插件应该失败")
	}
}

// TestPluginManager_RegisterWithInit 测试插件注册并初始化
func TestPluginManager_RegisterWithInit(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 测试正常注册并初始化
	p1 := newMockPlugin("test_plugin")
	params := map[string]string{"key": "value"}
	err := pm.RegisterWithInit(p1, params)
	if err != nil {
		t.Fatalf("注册并初始化插件失败: %v", err)
	}
	if p1.initParams["key"] != "value" {
		t.Fatal("初始化参数未正确传递")
	}

	// 测试初始化失败
	p2 := newMockPlugin("test_plugin2")
	p2.initError = errors.New("初始化失败")
	err = pm.RegisterWithInit(p2, params)
	if err == nil {
		t.Fatal("初始化失败应该返回错误")
	}
	// 验证插件已被移除
	_, exists := pm.GetPlugin("test_plugin2")
	if exists {
		t.Fatal("初始化失败后插件应该被移除")
	}
}

// TestPluginManager_Bind 测试插件绑定
func TestPluginManager_Bind(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 先注册插件
	p1 := newMockPlugin("test_plugin")
	err := pm.Register(p1)
	if err != nil {
		t.Fatalf("注册插件失败: %v", err)
	}

	// 测试正常绑定
	binding := plugin.PluginBinding{
		PluginName: "test_plugin",
		Event:      plugin.EventTaskSuccess,
		Params:     map[string]string{"key": "value"},
	}
	err = pm.Bind(binding)
	if err != nil {
		t.Fatalf("绑定插件失败: %v", err)
	}

	// 测试未注册插件绑定
	binding2 := plugin.PluginBinding{
		PluginName: "not_exist",
		Event:      plugin.EventTaskSuccess,
	}
	err = pm.Bind(binding2)
	if err == nil {
		t.Fatal("未注册插件绑定应该失败")
	}

	// 测试空名称绑定
	binding3 := plugin.PluginBinding{
		PluginName: "",
		Event:      plugin.EventTaskSuccess,
	}
	err = pm.Bind(binding3)
	if err == nil {
		t.Fatal("空名称绑定应该失败")
	}

	// 测试空事件绑定
	binding4 := plugin.PluginBinding{
		PluginName: "test_plugin",
		Event:      "",
	}
	err = pm.Bind(binding4)
	if err == nil {
		t.Fatal("空事件绑定应该失败")
	}
}

// TestPluginManager_Trigger 测试插件触发
func TestPluginManager_Trigger(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 注册插件
	p1 := newMockPlugin("test_plugin")
	err := pm.Register(p1)
	if err != nil {
		t.Fatalf("注册插件失败: %v", err)
	}

	// 绑定插件
	binding := plugin.PluginBinding{
		PluginName: "test_plugin",
		Event:      plugin.EventTaskSuccess,
	}
	err = pm.Bind(binding)
	if err != nil {
		t.Fatalf("绑定插件失败: %v", err)
	}

	// 触发插件
	pluginData := plugin.PluginData{
		Event:      plugin.EventTaskSuccess,
		WorkflowID: "wf1",
		InstanceID: "inst1",
		TaskID:     "task1",
		TaskName:   "test_task",
		Status:     "SUCCESS",
		Data:       map[string]interface{}{"result": "ok"},
	}
	err = pm.Trigger(context.Background(), plugin.EventTaskSuccess, pluginData)
	if err != nil {
		t.Fatalf("触发插件失败: %v", err)
	}

	// 验证插件被调用
	if p1.executeCallCount != 1 {
		t.Fatalf("插件应该被调用1次，实际: %d", p1.executeCallCount)
	}
	if p1.executeData == nil {
		t.Fatal("插件数据应该被传递")
	}

	// 测试未绑定事件
	err = pm.Trigger(context.Background(), plugin.EventTaskFailed, pluginData)
	if err != nil {
		t.Fatalf("未绑定事件应该返回nil，实际: %v", err)
	}
}

// TestPluginManager_TriggerWithCondition 测试带条件的插件触发
func TestPluginManager_TriggerWithCondition(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 注册插件
	p1 := newMockPlugin("test_plugin")
	err := pm.Register(p1)
	if err != nil {
		t.Fatalf("注册插件失败: %v", err)
	}

	// 绑定插件（带条件）
	binding := plugin.PluginBinding{
		PluginName: "test_plugin",
		Event:      plugin.EventTaskSuccess,
		Condition: func(data interface{}) bool {
			pluginData := data.(plugin.PluginData)
			return pluginData.TaskID == "task1"
		},
	}
	err = pm.Bind(binding)
	if err != nil {
		t.Fatalf("绑定插件失败: %v", err)
	}

	// 触发插件（满足条件）
	pluginData := plugin.PluginData{
		Event:      plugin.EventTaskSuccess,
		TaskID:     "task1",
		Status:     "SUCCESS",
	}
	err = pm.Trigger(context.Background(), plugin.EventTaskSuccess, pluginData)
	if err != nil {
		t.Fatalf("触发插件失败: %v", err)
	}
	if p1.executeCallCount != 1 {
		t.Fatalf("插件应该被调用1次，实际: %d", p1.executeCallCount)
	}

	// 触发插件（不满足条件）
	pluginData2 := plugin.PluginData{
		Event:      plugin.EventTaskSuccess,
		TaskID:     "task2",
		Status:     "SUCCESS",
	}
	err = pm.Trigger(context.Background(), plugin.EventTaskSuccess, pluginData2)
	if err != nil {
		t.Fatalf("触发插件失败: %v", err)
	}
	// 插件不应该被再次调用
	if p1.executeCallCount != 1 {
		t.Fatalf("插件不应该被再次调用，实际调用次数: %d", p1.executeCallCount)
	}
}

// TestPluginManager_Unregister 测试插件注销
func TestPluginManager_Unregister(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 注册插件
	p1 := newMockPlugin("test_plugin")
	err := pm.Register(p1)
	if err != nil {
		t.Fatalf("注册插件失败: %v", err)
	}

	// 绑定插件
	binding := plugin.PluginBinding{
		PluginName: "test_plugin",
		Event:      plugin.EventTaskSuccess,
	}
	err = pm.Bind(binding)
	if err != nil {
		t.Fatalf("绑定插件失败: %v", err)
	}

	// 注销插件
	err = pm.Unregister("test_plugin")
	if err != nil {
		t.Fatalf("注销插件失败: %v", err)
	}

	// 验证插件已不存在
	_, exists := pm.GetPlugin("test_plugin")
	if exists {
		t.Fatal("插件应该已被注销")
	}

	// 验证绑定也被移除
	pluginData := plugin.PluginData{
		Event:  plugin.EventTaskSuccess,
		TaskID: "task1",
	}
	err = pm.Trigger(context.Background(), plugin.EventTaskSuccess, pluginData)
	if err != nil {
		t.Fatalf("触发插件失败: %v", err)
	}
	// 插件不应该被调用
	if p1.executeCallCount != 0 {
		t.Fatalf("插件不应该被调用，实际调用次数: %d", p1.executeCallCount)
	}
}

// TestPluginManager_ListPlugins 测试列出插件
func TestPluginManager_ListPlugins(t *testing.T) {
	pm := plugin.NewPluginManager()
	
	// 注册多个插件
	p1 := newMockPlugin("plugin1")
	p2 := newMockPlugin("plugin2")
	p3 := newMockPlugin("plugin3")
	
	pm.Register(p1)
	pm.Register(p2)
	pm.Register(p3)

	// 列出插件
	plugins := pm.ListPlugins()
	if len(plugins) != 3 {
		t.Fatalf("应该有3个插件，实际: %d", len(plugins))
	}

	// 验证插件名称
	pluginMap := make(map[string]bool)
	for _, name := range plugins {
		pluginMap[name] = true
	}
	if !pluginMap["plugin1"] || !pluginMap["plugin2"] || !pluginMap["plugin3"] {
		t.Fatal("插件列表不完整")
	}
}

