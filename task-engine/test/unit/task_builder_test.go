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

	// 注意：同一个状态只能设置一个Handler，后设置的会覆盖前面的
	// 如果需要多个Handler，应该在Handler内部组合调用
	if len(taskInstance.StatusHandlers[task.TaskStatusSuccess]) == 0 {
		t.Logf("注意：同一个状态只能设置一个Handler，当前使用: %s", taskInstance.StatusHandlers[task.TaskStatusSuccess])
	}
}
