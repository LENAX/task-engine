package unit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stevelan1995/task-engine/pkg/core/builder"
	"github.com/stevelan1995/task-engine/pkg/core/task"
)

func TestTaskBuilder_Basic(t *testing.T) {
	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册一个模拟文件读写的函数（模拟IO操作）
	readFileFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟文件读取操作
		fileName := ctx.GetParam("fileName")
		if fileName == nil {
			return nil, fmt.Errorf("fileName参数不能为空")
		}

		// 模拟IO延迟
		time.Sleep(10 * time.Millisecond)

		// 模拟读取文件内容（实际场景中会读取真实文件）
		content := fmt.Sprintf("文件内容: %s", fileName)
		return map[string]interface{}{
			"fileName": fileName,
			"content":  content,
			"size":     len(content),
		}, nil
	}
	functionId, err := registry.Register(ctx, "readFile", readFileFunc, "读取文件内容")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	} else {
		fmt.Println("functionId: ", functionId)
	}

	task, err := builder.NewTaskBuilder("test-task", "测试任务", registry).
		WithJobFunction("readFile", map[string]interface{}{"fileName": "test.txt"}).
		WithTimeout(60).
		WithRetryCount(3).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	if task == nil {
		t.Fatal("Task为nil")
	}

	if task.Name != "test-task" {
		t.Errorf("Task名称错误，期望: test-task, 实际: %s", task.Name)
	}

	if task.JobFuncName != "readFile" {
		t.Errorf("Job函数名称错误，期望: readFile, 实际: %s", task.JobFuncName)
	}

	if task.TimeoutSeconds != 60 {
		t.Errorf("超时时间错误，期望: 60, 实际: %d", task.TimeoutSeconds)
	}

	if task.RetryCount != 3 {
		t.Errorf("重试次数错误，期望: 3, 实际: %d", task.RetryCount)
	}

	fileName, exists := task.Params.Load("fileName")
	if !exists {
		t.Error("参数fileName不存在")
	} else if fileName != "test.txt" {
		t.Errorf("参数错误，期望: test.txt, 实际: %v", fileName)
	}
}

func TestTaskBuilder_WithDependency(t *testing.T) {
	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 模拟网络请求操作
	httpRequestFunc := func(ctx *task.TaskContext) (interface{}, error) {
		url := ctx.GetParam("url")
		if url == nil {
			return nil, fmt.Errorf("url参数不能为空")
		}

		// 模拟网络IO延迟
		time.Sleep(20 * time.Millisecond)

		// 模拟HTTP请求响应
		return map[string]interface{}{
			"url":        url,
			"statusCode": 200,
			"body":       "响应内容",
		}, nil
	}
	_, err := registry.Register(ctx, "httpRequest", httpRequestFunc, "发送HTTP请求")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task, err := builder.NewTaskBuilder("task2", "任务2", registry).
		WithJobFunction("httpRequest", map[string]interface{}{"url": "https://www.baidu.com"}).
		WithDependency("task1").
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 1 {
		t.Fatalf("依赖数量错误，期望: 1, 实际: %d", len(deps))
	}

	if deps[0] != "task1" {
		t.Errorf("依赖名称错误，期望: task1, 实际: %s", deps[0])
	}
}

func TestTaskBuilder_WithDependencies(t *testing.T) {
	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 模拟数据库查询操作
	dbQueryFunc := func(ctx *task.TaskContext) (interface{}, error) {
		query := ctx.GetParam("query")
		if query == nil {
			return nil, fmt.Errorf("query参数不能为空")
		}

		// 模拟数据库IO延迟
		time.Sleep(15 * time.Millisecond)

		// 模拟数据库查询结果
		return map[string]interface{}{
			"query": query,
			"rows":  []map[string]interface{}{{"id": 1, "name": "test"}},
			"count": 1,
		}, nil
	}
	_, err := registry.Register(ctx, "dbQuery", dbQueryFunc, "数据库查询")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task, err := builder.NewTaskBuilder("task3", "任务3", registry).
		WithJobFunction("dbQuery", map[string]interface{}{"query": "SELECT * FROM users"}).
		WithDependencies([]string{"task1", "task2"}).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 2 {
		t.Fatalf("依赖数量错误，期望: 2, 实际: %d", len(deps))
	}
}

func TestTaskBuilder_EmptyName(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	// WithJobFunction 不再返回错误，校验延迟到 Build() 时进行
	// Build 应该返回错误（因为名称为空或函数未注册）
	_, err := builder.NewTaskBuilder("", "描述", registry).
		WithJobFunction("func", nil).
		Build()
	if err == nil {
		t.Fatal("期望返回错误（名称为空或函数未注册），但未返回")
	}
}

func TestTaskBuilder_EmptyJobFuncName(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	builder := builder.NewTaskBuilder("task", "描述", registry)

	_, err := builder.Build()

	if err == nil {
		t.Fatal("期望返回错误，但未返回")
	}
}

// TestTaskBuilder_UnregisteredFunction 测试未注册函数的情况
func TestTaskBuilder_UnregisteredFunction(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)

	// 尝试使用未注册的函数构建任务
	_, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("unregisteredFunc", nil).
		Build()

	if err == nil {
		t.Fatal("期望返回错误（函数未注册），但未返回")
	}

	// 验证错误消息包含函数名称
	if err.Error() != "Job函数 unregisteredFunc 未在registry中注册" {
		t.Errorf("错误消息不正确，期望包含 '未在registry中注册'，实际: %v", err)
	}
}

func TestTaskBuilder_DefaultValues(t *testing.T) {
	// 创建 registry 并注册函数
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 模拟文件写入操作
	writeFileFunc := func(ctx *task.TaskContext) (interface{}, error) {
		// 模拟文件写入IO操作
		time.Sleep(5 * time.Millisecond)
		return map[string]interface{}{
			"written": true,
			"bytes":   1024,
		}, nil
	}
	_, err := registry.Register(ctx, "writeFile", writeFileFunc, "写入文件")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("writeFile", nil).
		Build()

	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	if task.TimeoutSeconds != 30 {
		t.Errorf("默认超时时间错误，期望: 30, 实际: %d", task.TimeoutSeconds)
	}

	if task.RetryCount != 0 {
		t.Errorf("默认重试次数错误，期望: 0, 实际: %d", task.RetryCount)
	}
}

// TestTaskBuilder_WithTaskHandler_Logging 测试使用日志Handler
func TestTaskBuilder_WithTaskHandler_Logging(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册一个模拟IO操作的函数（文件下载）
	downloadFileFunc := func(ctx *task.TaskContext) (interface{}, error) {
		url := ctx.GetParam("url")
		if url == nil {
			return nil, fmt.Errorf("url参数不能为空")
		}

		// 模拟网络IO延迟
		time.Sleep(30 * time.Millisecond)

		// 模拟下载文件
		return map[string]interface{}{
			"url":      url,
			"fileSize": 1024 * 1024, // 1MB
			"status":   "completed",
		}, nil
	}
	_, err := registry.Register(ctx, "downloadFile", downloadFileFunc, "下载文件")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 注册日志Handler（模拟日志记录操作）
	logHandler := func(ctx *task.TaskContext) {
		taskID := ctx.TaskID
		taskName := ctx.TaskName
		status := ctx.GetParam("_status")
		result := ctx.GetParam("result")

		// 模拟日志写入操作（实际场景中会写入日志文件或发送到日志服务）
		logMsg := fmt.Sprintf("[%s] Task %s (%s) 执行完成, 状态: %v, 结果: %v",
			time.Now().Format("2006-01-02 15:04:05"), taskName, taskID, status, result)
		_ = logMsg // 实际场景中会写入日志
		// 这里只是模拟，不实际写入，避免测试时产生文件
	}

	_, err = registry.RegisterTaskHandler(ctx, "logHandler", logHandler, "日志记录Handler")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建TaskBuilder并设置Handler
	taskInstance, err := builder.NewTaskBuilder("download-task", "下载任务", registry).
		WithJobFunction("downloadFile", map[string]interface{}{"url": "https://example.com/file.zip"}).
		WithTaskHandler(task.TaskStatusSuccess, "logHandler").
		WithTaskHandler(task.TaskStatusFailed, "logHandler").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证Handler已设置
	if taskInstance.StatusHandlers == nil {
		t.Fatal("StatusHandlers应该不为nil")
	}

	if len(taskInstance.StatusHandlers[task.TaskStatusSuccess]) == 0 {
		t.Error("成功状态的Handler未设置")
	}

	if len(taskInstance.StatusHandlers[task.TaskStatusFailed]) == 0 {
		t.Error("失败状态的Handler未设置")
	}
}

// TestTaskBuilder_WithTaskHandler_Database 测试使用数据库Handler
func TestTaskBuilder_WithTaskHandler_Database(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册一个模拟IO操作的函数（数据处理）
	processDataFunc := func(ctx *task.TaskContext) (interface{}, error) {
		data := ctx.GetParam("data")
		if data == nil {
			return nil, fmt.Errorf("data参数不能为空")
		}

		// 模拟数据处理IO操作
		time.Sleep(25 * time.Millisecond)

		// 模拟处理结果
		return map[string]interface{}{
			"processed": true,
			"records":   100,
			"data":      data,
		}, nil
	}
	_, err := registry.Register(ctx, "processData", processDataFunc, "处理数据")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 注册数据库Handler（模拟数据库操作）
	dbHandler := func(ctx *task.TaskContext) {
		taskID := ctx.TaskID
		taskName := ctx.TaskName
		status := ctx.GetParam("_status")
		result := ctx.GetParam("result")
		errorMsg := ctx.GetParam("error")

		// 模拟数据库写入操作（实际场景中会写入数据库）
		// 记录任务执行结果到数据库
		dbRecord := map[string]interface{}{
			"task_id":    taskID,
			"task_name":  taskName,
			"status":     status,
			"result":     result,
			"error":      errorMsg,
			"created_at": time.Now(),
		}
		_ = dbRecord // 实际场景中会执行 INSERT INTO task_logs ...

		// 模拟数据库IO延迟
		time.Sleep(5 * time.Millisecond)
	}

	_, err = registry.RegisterTaskHandler(ctx, "dbHandler", dbHandler, "数据库记录Handler")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 创建TaskBuilder并设置Handler
	taskInstance, err := builder.NewTaskBuilder("process-task", "处理任务", registry).
		WithJobFunction("processData", map[string]interface{}{"data": "test data"}).
		WithTaskHandler(task.TaskStatusSuccess, "dbHandler").
		WithTaskHandler(task.TaskStatusFailed, "dbHandler").
		WithTaskHandler(task.TaskStatusTimeout, "dbHandler").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证Handler已设置
	if len(taskInstance.StatusHandlers) != 3 {
		t.Errorf("Handler数量错误，期望: 3, 实际: %d", len(taskInstance.StatusHandlers))
	}

	if len(taskInstance.StatusHandlers[task.TaskStatusSuccess]) == 0 {
		t.Error("成功状态的Handler未设置")
	}

	if len(taskInstance.StatusHandlers[task.TaskStatusFailed]) == 0 {
		t.Error("失败状态的Handler未设置")
	}

	if len(taskInstance.StatusHandlers[task.TaskStatusTimeout]) == 0 {
		t.Error("超时状态的Handler未设置")
	}
}

// TestTaskBuilder_WithTaskHandler_Combined 测试组合使用多个Handler
func TestTaskBuilder_WithTaskHandler_Combined(t *testing.T) {
	// 创建 registry
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	// 注册一个模拟IO操作的函数（文件上传）
	uploadFileFunc := func(ctx *task.TaskContext) (interface{}, error) {
		filePath := ctx.GetParam("filePath")
		if filePath == nil {
			return nil, fmt.Errorf("filePath参数不能为空")
		}

		// 模拟文件IO操作
		time.Sleep(40 * time.Millisecond)

		// 模拟上传结果
		return map[string]interface{}{
			"filePath": filePath,
			"uploaded": true,
			"url":      "https://storage.example.com/files/123",
		}, nil
	}
	_, err := registry.Register(ctx, "uploadFile", uploadFileFunc, "上传文件")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 注册日志Handler
	logHandler := func(ctx *task.TaskContext) {
		taskID := ctx.TaskID
		status := ctx.GetParam("_status")
		// 模拟日志写入
		_ = fmt.Sprintf("Task %s status: %v", taskID, status)
	}
	_, err = registry.RegisterTaskHandler(ctx, "logHandler", logHandler, "日志Handler")
	if err != nil {
		t.Fatalf("注册日志Handler失败: %v", err)
	}

	// 注册数据库Handler
	dbHandler := func(ctx *task.TaskContext) {
		taskID := ctx.TaskID
		result := ctx.GetParam("result")
		// 模拟数据库写入
		_ = map[string]interface{}{
			"task_id": taskID,
			"result":  result,
		}
	}
	_, err = registry.RegisterTaskHandler(ctx, "dbHandler", dbHandler, "数据库Handler")
	if err != nil {
		t.Fatalf("注册数据库Handler失败: %v", err)
	}

	// 创建TaskBuilder并设置多个Handler
	taskInstance, err := builder.NewTaskBuilder("upload-task", "上传任务", registry).
		WithJobFunction("uploadFile", map[string]interface{}{"filePath": "/tmp/file.txt"}).
		WithTaskHandler(task.TaskStatusSuccess, "logHandler").
		WithTaskHandler(task.TaskStatusSuccess, "dbHandler").
		WithTaskHandler(task.TaskStatusFailed, "logHandler").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证Handler已设置
	if len(taskInstance.StatusHandlers[task.TaskStatusSuccess]) == 0 {
		t.Error("成功状态的Handler未设置")
	}

	// 验证同一个状态可以设置多个Handler（按顺序执行）
	if len(taskInstance.StatusHandlers[task.TaskStatusSuccess]) != 2 {
		t.Errorf("成功状态应该有两个Handler，实际: %d", len(taskInstance.StatusHandlers[task.TaskStatusSuccess]))
	}
}

// TestTaskBuilder_NilRegistry 测试nil registry的情况（应该panic）
func TestTaskBuilder_NilRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("期望NewTaskBuilder在registry为nil时panic，但未panic")
		}
	}()

	builder.NewTaskBuilder("task", "描述", nil)
}

// TestTaskBuilder_InvalidTimeout 测试无效的超时时间
func TestTaskBuilder_InvalidTimeout(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试负数超时时间（应该使用默认值30）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithTimeout(-1).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}
	if task.TimeoutSeconds != 30 {
		t.Errorf("负数超时时间应该使用默认值30，实际: %d", task.TimeoutSeconds)
	}

	// 测试0超时时间（0是有效值，不会被替换为默认值）
	task, err = builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithTimeout(0).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}
	if task.TimeoutSeconds != 0 {
		t.Errorf("0超时时间应该保持为0，实际: %d", task.TimeoutSeconds)
	}
}

// TestTaskBuilder_InvalidRetryCount 测试无效的重试次数
func TestTaskBuilder_InvalidRetryCount(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试负数重试次数（应该使用默认值0）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithRetryCount(-1).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}
	if task.RetryCount != 0 {
		t.Errorf("负数重试次数应该使用默认值0，实际: %d", task.RetryCount)
	}
}

// TestTaskBuilder_DuplicateDependency 测试重复依赖的去重
func TestTaskBuilder_DuplicateDependency(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试单个依赖重复添加
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("task1").
		WithDependency("task1").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 1 {
		t.Errorf("重复依赖应该去重，期望1个，实际: %d", len(deps))
	}
	if deps[0] != "task1" {
		t.Errorf("依赖名称错误，期望: task1, 实际: %s", deps[0])
	}

	// 测试批量依赖中包含重复项
	task2, err := builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithDependencies([]string{"task1", "task2", "task1", "task3"}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps2 := task2.GetDependencies()
	if len(deps2) != 3 {
		t.Errorf("批量依赖去重后应该为3个，实际: %d", len(deps2))
	}
}

// TestTaskBuilder_EmptyDependency 测试空依赖
func TestTaskBuilder_EmptyDependency(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试空字符串依赖（应该被忽略）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithDependency("").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps := task.GetDependencies()
	if len(deps) != 0 {
		t.Errorf("空依赖应该被忽略，期望0个，实际: %d", len(deps))
	}

	// 测试空依赖列表
	task2, err := builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithDependencies([]string{}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps2 := task2.GetDependencies()
	if len(deps2) != 0 {
		t.Errorf("空依赖列表应该被忽略，期望0个，实际: %d", len(deps2))
	}

	// 测试包含空字符串的依赖列表
	task3, err := builder.NewTaskBuilder("task3", "描述3", registry).
		WithJobFunction("mockFunc", nil).
		WithDependencies([]string{"task1", "", "task2"}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	deps3 := task3.GetDependencies()
	if len(deps3) != 2 {
		t.Errorf("包含空字符串的依赖列表应该过滤空项，期望2个，实际: %d", len(deps3))
	}
}

// TestTaskBuilder_UnregisteredTaskHandler 测试未注册的TaskHandler
func TestTaskBuilder_UnregisteredTaskHandler(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试未注册的Handler（应该在Build时返回错误）
	_, err = builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler(task.TaskStatusSuccess, "unregisteredHandler").
		Build()
	if err == nil {
		t.Fatal("期望返回错误（Handler未注册），但未返回")
	}
}

// TestTaskBuilder_EmptyTaskHandler 测试空的TaskHandler名称或状态
func TestTaskBuilder_EmptyTaskHandler(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	handler := func(ctx *task.TaskContext) {}
	_, err = registry.RegisterTaskHandler(ctx, "testHandler", handler, "测试Handler")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 测试空状态（应该被忽略）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler("", "testHandler").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}
	if task.StatusHandlers != nil && len(task.StatusHandlers) > 0 {
		t.Error("空状态的Handler应该被忽略")
	}

	// 测试空Handler名称（应该被忽略）
	task2, err := builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler("SUCCESS", "").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}
	if task2.StatusHandlers != nil && len(task2.StatusHandlers) > 0 {
		t.Error("空Handler名称应该被忽略")
	}
}

// TestTaskBuilder_WithRequiredParams 测试必需参数列表
func TestTaskBuilder_WithRequiredParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试设置必需参数
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithRequiredParams([]string{"param1", "param2", "param3"}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	requiredParams := task.GetRequiredParams()
	if len(requiredParams) != 3 {
		t.Errorf("必需参数数量错误，期望: 3, 实际: %d", len(requiredParams))
	}

	// 验证参数顺序
	if requiredParams[0] != "param1" || requiredParams[1] != "param2" || requiredParams[2] != "param3" {
		t.Error("必需参数顺序错误")
	}

	// 测试空必需参数列表
	task2, err := builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithRequiredParams([]string{}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	requiredParams2 := task2.GetRequiredParams()
	if len(requiredParams2) != 0 {
		t.Errorf("空必需参数列表应该被忽略，期望: 0, 实际: %d", len(requiredParams2))
	}
}

// TestTaskBuilder_WithResultMapping 测试结果映射
func TestTaskBuilder_WithResultMapping(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试设置结果映射
	mapping := map[string]string{
		"result.field1": "param1",
		"result.field2": "param2",
		"data.value":    "param3",
	}
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithResultMapping(mapping).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	resultMapping := task.GetResultMapping()
	if len(resultMapping) != 3 {
		t.Errorf("结果映射数量错误，期望: 3, 实际: %d", len(resultMapping))
	}

	// 验证映射内容
	if resultMapping["result.field1"] != "param1" {
		t.Error("结果映射内容错误")
	}
	if resultMapping["result.field2"] != "param2" {
		t.Error("结果映射内容错误")
	}
	if resultMapping["data.value"] != "param3" {
		t.Error("结果映射内容错误")
	}

	// 测试空结果映射
	task2, err := builder.NewTaskBuilder("task2", "描述2", registry).
		WithJobFunction("mockFunc", nil).
		WithResultMapping(map[string]string{}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	resultMapping2 := task2.GetResultMapping()
	if len(resultMapping2) != 0 {
		t.Errorf("空结果映射应该被忽略，期望: 0, 实际: %d", len(resultMapping2))
	}
}

// TestTaskBuilder_ParameterTypeConversion 测试参数类型转换
func TestTaskBuilder_ParameterTypeConversion(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试各种类型的参数转换
	params := map[string]interface{}{
		"stringParam": "test",
		"intParam":    123,
		"floatParam":  45.67,
		"boolParam":   true,
		"nilParam":    nil,
		"sliceParam":  []int{1, 2, 3},
		"mapParam":    map[string]int{"key": 1},
	}

	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", params).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证字符串参数
	if val, ok := task.Params.Load("stringParam"); !ok || val != "test" {
		t.Errorf("字符串参数错误，期望: test, 实际: %v", val)
	}

	// 验证整数参数（应该转换为字符串）
	if val, ok := task.Params.Load("intParam"); !ok || val != "123" {
		t.Errorf("整数参数转换错误，期望: 123, 实际: %v", val)
	}

	// 验证浮点数参数（应该转换为字符串）
	if val, ok := task.Params.Load("floatParam"); !ok || val != "45.67" {
		t.Errorf("浮点数参数转换错误，期望: 45.67, 实际: %v", val)
	}

	// 验证布尔参数（应该转换为字符串）
	if val, ok := task.Params.Load("boolParam"); !ok || val != "true" {
		t.Errorf("布尔参数转换错误，期望: true, 实际: %v", val)
	}

	// 验证nil参数（应该转换为空字符串）
	if val, ok := task.Params.Load("nilParam"); !ok || val != "" {
		t.Errorf("nil参数转换错误，期望: 空字符串, 实际: %v", val)
	}

	// 验证切片参数（应该转换为字符串）
	if val, ok := task.Params.Load("sliceParam"); !ok {
		t.Error("切片参数不存在")
	} else {
		// 切片会被转换为字符串表示
		valStr := val.(string)
		if valStr == "" {
			t.Error("切片参数转换后不应该为空")
		}
	}

	// 验证map参数（应该转换为字符串）
	if val, ok := task.Params.Load("mapParam"); !ok {
		t.Error("map参数不存在")
	} else {
		// map会被转换为字符串表示
		valStr := val.(string)
		if valStr == "" {
			t.Error("map参数转换后不应该为空")
		}
	}
}

// TestTaskBuilder_NilParams 测试nil参数
func TestTaskBuilder_NilParams(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	// 测试nil参数（应该创建空的params map）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证params不为nil（即使传入nil，也应该创建空的map）
	// sync.Map不能与nil比较，我们通过检查是否能存储值来验证
	task.Params.Store("test", "value")
	if val, ok := task.Params.Load("test"); !ok || val != "value" {
		t.Error("Params应该可以正常使用")
	}
}

// TestTaskBuilder_EmptyJobFunctionName 测试空的Job函数名称
func TestTaskBuilder_EmptyJobFunctionName(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)

	// 测试空函数名称（应该被忽略，Build时才会报错）
	builder := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("", nil)

	// Build时应该返回错误（因为函数名称为空）
	_, err := builder.Build()
	if err == nil {
		t.Fatal("期望返回错误（Job函数名称为空），但未返回")
	}
}

// TestTaskBuilder_ChainedCalls 测试链式调用所有方法
func TestTaskBuilder_ChainedCalls(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	handler := func(ctx *task.TaskContext) {}
	_, err = registry.RegisterTaskHandler(ctx, "testHandler", handler, "测试Handler")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 测试链式调用所有方法
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", map[string]interface{}{"param1": "value1"}).
		WithTimeout(60).
		WithRetryCount(3).
		WithDependency("task1").
		WithDependencies([]string{"task2", "task3"}).
		WithTaskHandler(task.TaskStatusSuccess, "testHandler").
		WithTaskHandler(task.TaskStatusFailed, "testHandler").
		WithRequiredParams([]string{"param1", "param2"}).
		WithResultMapping(map[string]string{"result.field": "param"}).
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证所有设置
	if task.Name != "task" {
		t.Error("Task名称错误")
	}
	if task.JobFuncName != "mockFunc" {
		t.Error("Job函数名称错误")
	}
	if task.TimeoutSeconds != 60 {
		t.Error("超时时间错误")
	}
	if task.RetryCount != 3 {
		t.Error("重试次数错误")
	}
	if len(task.GetDependencies()) != 3 {
		t.Error("依赖数量错误")
	}
	if len(task.StatusHandlers) != 2 {
		t.Error("Handler数量错误")
	}
	if len(task.GetRequiredParams()) != 2 {
		t.Error("必需参数数量错误")
	}
	if len(task.GetResultMapping()) != 1 {
		t.Error("结果映射数量错误")
	}
}

// TestTaskBuilder_MultipleHandlersForSameStatus 测试同一状态设置多个Handler
func TestTaskBuilder_MultipleHandlersForSameStatus(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	handler1 := func(ctx *task.TaskContext) {}
	handler2 := func(ctx *task.TaskContext) {}
	_, err = registry.RegisterTaskHandler(ctx, "handler1", handler1, "Handler1")
	if err != nil {
		t.Fatalf("注册Handler1失败: %v", err)
	}
	_, err = registry.RegisterTaskHandler(ctx, "handler2", handler2, "Handler2")
	if err != nil {
		t.Fatalf("注册Handler2失败: %v", err)
	}

	// 测试同一状态设置多个Handler（应该按顺序添加）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler("SUCCESS", "handler1").
		WithTaskHandler("SUCCESS", "handler2").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证同一状态有多个Handler
	handlers := task.StatusHandlers["SUCCESS"]
	if len(handlers) != 2 {
		t.Errorf("同一状态应该有两个Handler，实际: %d", len(handlers))
	}
}

// TestTaskBuilder_DuplicateHandlerForSameStatus 测试同一状态重复添加相同Handler
func TestTaskBuilder_DuplicateHandlerForSameStatus(t *testing.T) {
	registry := task.NewFunctionRegistry(nil, nil)
	ctx := context.Background()

	mockFunc := func(ctx *task.TaskContext) (interface{}, error) {
		return "success", nil
	}
	_, err := registry.Register(ctx, "mockFunc", mockFunc, "模拟函数")
	if err != nil {
		t.Fatalf("注册函数失败: %v", err)
	}

	handler := func(ctx *task.TaskContext) {}
	_, err = registry.RegisterTaskHandler(ctx, "testHandler", handler, "测试Handler")
	if err != nil {
		t.Fatalf("注册Handler失败: %v", err)
	}

	// 测试同一状态重复添加相同Handler（应该去重）
	task, err := builder.NewTaskBuilder("task", "描述", registry).
		WithJobFunction("mockFunc", nil).
		WithTaskHandler("SUCCESS", "testHandler").
		WithTaskHandler("SUCCESS", "testHandler").
		Build()
	if err != nil {
		t.Fatalf("构建Task失败: %v", err)
	}

	// 验证重复Handler被去重
	handlers := task.StatusHandlers["SUCCESS"]
	if len(handlers) != 1 {
		t.Errorf("重复Handler应该去重，期望1个，实际: %d", len(handlers))
	}
}
