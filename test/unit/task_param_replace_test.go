package unit

import (
	"context"
	"testing"

	"github.com/LENAX/task-engine/pkg/core/builder"
	"github.com/LENAX/task-engine/pkg/core/task"
)

func TestTask_ReplaceParams(t *testing.T) {
	// 创建registry（mock）
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册一个测试函数
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	// 创建Task，使用占位符参数
	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"data_source_id": "${data_source_id}",
		"date_range":     "${date_range}",
		"static_value":   "static",
	})

	task, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 测试参数替换
	params := map[string]interface{}{
		"data_source_id": "ds_001",
		"date_range":     "2024-01-01,2024-01-31",
	}

	err = task.ReplaceParams(params)
	if err != nil {
		t.Fatalf("替换参数失败: %v", err)
	}

	// 验证参数是否被正确替换
	actualParams := task.GetParams()
	if actualParams["data_source_id"] != "ds_001" {
		t.Errorf("期望 data_source_id = 'ds_001', 实际 = '%v'", actualParams["data_source_id"])
	}
	if actualParams["date_range"] != "2024-01-01,2024-01-31" {
		t.Errorf("期望 date_range = '2024-01-01,2024-01-31', 实际 = '%v'", actualParams["date_range"])
	}
	if actualParams["static_value"] != "static" {
		t.Errorf("期望 static_value = 'static', 实际 = '%v'", actualParams["static_value"])
	}
}

func TestTask_ReplaceParams_Unreplaced(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
		"param2": "${param2}",
	})

	task, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 只提供部分参数
	params := map[string]interface{}{
		"param1": "value1",
		// param2 未提供
	}

	err = task.ReplaceParams(params)
	if err == nil {
		t.Error("期望返回错误，因为存在未替换的占位符")
	}

	// 验证param1被替换，param2保持原样
	actualParams := task.GetParams()
	if actualParams["param1"] != "value1" {
		t.Errorf("期望 param1 = 'value1', 实际 = '%v'", actualParams["param1"])
	}
	if actualParams["param2"] != "${param2}" {
		t.Errorf("期望 param2 = '${param2}', 实际 = '%v'", actualParams["param2"])
	}
}

func TestTask_ReplaceParams_EmptyParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
	})

	task, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 空参数应该不报错
	err = task.ReplaceParams(nil)
	if err != nil {
		t.Errorf("空参数不应该返回错误: %v", err)
	}

	err = task.ReplaceParams(map[string]interface{}{})
	if err != nil {
		t.Errorf("空参数map不应该返回错误: %v", err)
	}
}

func TestTask_ReplaceParams_NonStringValue(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"param1": "${param1}",
		"param2": 123, // 非字符串值
	})

	task, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	params := map[string]interface{}{
		"param1": "value1",
	}

	err = task.ReplaceParams(params)
	if err != nil {
		t.Fatalf("替换参数失败: %v", err)
	}

	// 验证非字符串值不受影响
	actualParams := task.GetParams()
	if actualParams["param2"] != "123" {
		t.Errorf("期望 param2 = '123', 实际 = '%v'", actualParams["param2"])
	}
}

func TestTask_ReplaceParams_TypeConversion(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()
	testFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	registry.Register(ctx, "TestFunc", testFunc, "测试函数")

	taskBuilder := builder.NewTaskBuilder("TestTask", "测试任务", registry)
	taskBuilder = taskBuilder.WithJobFunction("TestFunc", map[string]interface{}{
		"int_param":    "${int_param}",
		"bool_param":   "${bool_param}",
		"float_param":  "${float_param}",
		"string_param": "${string_param}",
	})

	task, err := taskBuilder.Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	params := map[string]interface{}{
		"int_param":    42,
		"bool_param":   true,
		"float_param":  3.14,
		"string_param": "test",
	}

	err = task.ReplaceParams(params)
	if err != nil {
		t.Fatalf("替换参数失败: %v", err)
	}

	actualParams := task.GetParams()
	if actualParams["int_param"] != "42" {
		t.Errorf("期望 int_param = '42', 实际 = '%v'", actualParams["int_param"])
	}
	if actualParams["bool_param"] != "true" {
		t.Errorf("期望 bool_param = 'true', 实际 = '%v'", actualParams["bool_param"])
	}
	if actualParams["float_param"] != "3.14" {
		t.Errorf("期望 float_param = '3.14', 实际 = '%v'", actualParams["float_param"])
	}
	if actualParams["string_param"] != "test" {
		t.Errorf("期望 string_param = 'test', 实际 = '%v'", actualParams["string_param"])
	}
}
