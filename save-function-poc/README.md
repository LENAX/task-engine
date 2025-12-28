# 任务函数持久化和加载 POC

这是一个最小的 proof of concept，用于验证任务函数的持久化和加载功能。

## 功能特性

1. **任务定义**: 定义任务，包含名称、描述、关联的函数ID和参数
2. **函数注册**: 注册用户函数，自动生成唯一ID并保存元数据
3. **异步执行**: 函数被包装为返回channel的异步函数，通过channel获取执行状态和结果
4. **SQLite持久化**: 将任务和函数元数据保存到SQLite数据库
5. **重启加载**: 从数据库加载任务和函数，确保重启后能正常运行
6. **IO操作模拟**: 示例函数包含模拟的网络IO、文件IO等操作

## 项目结构

```
save-function-poc/
├── main.go              # 主入口，命令行工具
├── task.go              # 任务定义
├── function_registry.go # 函数注册中心
├── storage.go           # SQLite存储实现
├── user_function.go     # 用户函数示例
├── save_task.go         # 保存任务和函数的脚本
├── load_and_run.go      # 加载和运行任务的脚本
└── go.mod               # Go模块定义
```

## 使用方法

### 快速开始

#### 方式1: 使用命令行工具（演示用）

```bash
# 安装依赖
go mod download

# 保存任务和函数（旧API）
go run . save

# 加载并执行任务（旧API）
go run . load

# 使用新API（推荐）
go run . save2
go run . load2
```

#### 方式2: 在代码中使用（推荐）

参考 `example_usage.go` 文件，包含完整的使用示例。

### 用户使用指南

#### 1. 定义函数

在您的package中定义函数（例如 `user_functions.go`）：

```go
package main

import "context"

// 定义您的任务函数
func MyTask(ctx context.Context, param1 string, param2 int) (string, error) {
    // 您的业务逻辑
    return fmt.Sprintf("处理: %s, %d", param1, param2), nil
}
```

#### 2. 在main.go中初始化

```go
package main

import (
    "context"
    "log"
)

func main() {
    ctx := context.Background()
    dbPath := "tasks.db"
    
    // 1. 创建存储和注册中心
    storage, err := NewStorage(dbPath)
    if err != nil {
        log.Fatalf("创建存储失败: %v", err)
    }
    defer storage.Close()
    
    registry := NewFunctionRegistry(storage)
    
    // 2. 批量注册所有函数（程序启动时）
    functions := []FunctionDef{
        {Name: "MyTask", Description: "我的任务", Function: MyTask},
        // 添加更多函数...
    }
    
    if err := registry.RegisterBatch(ctx, functions); err != nil {
        log.Fatalf("批量注册函数失败: %v", err)
    }
    
    // 3. 恢复已存在的函数（如果程序重启）
    funcMap := map[string]interface{}{
        "MyTask": MyTask,
        // 添加更多函数映射...
    }
    
    if err := registry.RestoreFunctions(ctx, funcMap); err != nil {
        log.Fatalf("恢复函数失败: %v", err)
    }
    
    // 4. 创建任务 - 方式1: 通过函数引用（自动注册）
    task1, err := NewTaskWithFunction(ctx, registry,
        "任务名称",
        "任务描述",
        MyTask,
        "MyTask",      // 函数名称
        "我的任务",     // 函数描述
    )
    if err != nil {
        log.Fatalf("创建任务失败: %v", err)
    }
    task1.Params["arg0"] = "value1"
    task1.Params["arg1"] = "100"
    
    // 5. 创建任务 - 方式2: 通过函数ID（函数已注册）
    funcID := registry.GetIDByName("MyTask")
    task2 := NewTask("另一个任务", "任务描述", funcID)
    task2.Params["arg0"] = "value2"
    
    // 6. 保存任务
    if err := storage.SaveTask(ctx, task1); err != nil {
        log.Fatalf("保存任务失败: %v", err)
    }
    
    // 7. 执行任务
    fn := registry.Get(task1.FuncID)
    if fn != nil {
        stateCh := fn(ctx, task1.Params)
        state := <-stateCh
        if state.Status == "Success" {
            log.Printf("任务执行成功: %v", state.Data)
        } else {
            log.Printf("任务执行失败: %v", state.Error)
        }
    }
}
```

#### 3. 加载并执行已保存的任务

```go
// 从数据库加载所有任务
tasks, err := storage.ListAllTasks(ctx)
if err != nil {
    log.Fatalf("加载任务失败: %v", err)
}

// 执行所有任务
for _, task := range tasks {
    fn := registry.Get(task.FuncID)
    if fn == nil {
        log.Printf("任务 %s 的函数未找到", task.Name)
        continue
    }
    
    stateCh := fn(ctx, task.Params)
    state := <-stateCh
    if state.Status == "Success" {
        log.Printf("任务 %s 执行成功: %v", task.Name, state.Data)
    } else {
        log.Printf("任务 %s 执行失败: %v", task.Name, state.Error)
    }
}

## 示例输出

### 保存任务

```
=== 开始注册函数和保存任务 ===
✓ 注册函数 AddNumbers, ID: xxx-xxx-xxx
✓ 注册函数 GreetUser, ID: xxx-xxx-xxx
✓ 注册函数 ProcessData, ID: xxx-xxx-xxx
✓ 注册函数 DownloadFile, ID: xxx-xxx-xxx
✓ 保存任务: 计算任务 (ID: xxx-xxx-xxx)
✓ 保存任务: 问候任务 (ID: xxx-xxx-xxx)
✓ 保存任务: 数据处理任务 (ID: xxx-xxx-xxx)
✓ 保存任务: 文件下载任务 (ID: xxx-xxx-xxx)

=== 保存完成 ===
数据库文件: tasks.db
已注册函数数: 4
```

### 加载并执行

```
=== 开始加载任务和函数 ===
✓ 从数据库加载了 4 个函数元数据
✓ 加载函数实例: AddNumbers (ID: xxx-xxx-xxx)
✓ 加载函数实例: GreetUser (ID: xxx-xxx-xxx)
✓ 加载函数实例: ProcessData (ID: xxx-xxx-xxx)
✓ 加载函数实例: DownloadFile (ID: xxx-xxx-xxx)

✓ 从数据库加载了 4 个任务

=== 开始执行任务 ===

[任务 1] 计算任务 (ID: xxx-xxx-xxx)
  描述: 计算两个数字的和
  函数ID: xxx-xxx-xxx
  函数名称: AddNumbers
  参数: map[arg0:10 arg1:20]
  开始执行...
[AddNumbers] 开始计算: 10 + 20
[AddNumbers] 计算结果: 30
  ✓ 执行成功，状态: Success，结果: 30

[任务 2] 问候任务 (ID: xxx-xxx-xxx)
  开始执行...
[GreetUser] 开始处理用户: Alice
[GreetUser] 网络请求失败，使用本地处理: ...
[GreetUser] 生成问候: Hello, Alice!
  ✓ 执行成功，状态: Success，结果: Hello, Alice!

[任务 4] 文件下载任务 (ID: xxx-xxx-xxx)
  开始执行...
[DownloadFile] 开始下载: https://example.com/file.zip (timeout: 3s)
[DownloadFile] 下载进度: 20%
[DownloadFile] 下载进度: 40%
[DownloadFile] 下载进度: 60%
[DownloadFile] 下载进度: 80%
[DownloadFile] 下载进度: 100%
[DownloadFile] 下载完成: Downloaded: ...
  ✓ 执行成功，状态: Success，结果: Downloaded: ...
```

## 核心组件说明

### Task
任务定义，包含：
- ID: 唯一标识
- Name: 任务名称
- Description: 任务描述
- FuncID: 关联的函数ID
- Params: 任务参数（map[string]string）

### JobFunctionState
函数执行状态，包含：
- Status: 执行状态（"Success" 或 "Failed"）
- Data: 执行结果（成功时）
- Error: 错误信息（失败时）

### TaskFunction
任务函数类型，统一签名：
```go
type TaskFunction func(ctx context.Context, params map[string]string) <-chan JobFunctionState
```
函数返回channel，通过channel异步获取执行状态和结果。

### FunctionRegistry
函数注册中心，负责：
- 注册函数并生成唯一ID
- 包装函数为统一签名 `TaskFunction`（返回channel）
- 从数据库加载函数元数据
- 管理函数实例的内存映射

### Storage
SQLite存储实现，提供：
- 函数元数据的CRUD操作
- 任务的CRUD操作
- 数据库表的自动创建

## 注意事项

1. **函数签名要求**:
   - 第一个参数必须是 `context.Context`
   - 最后一个返回值必须是 `error`
   - 支持基本类型参数（string, int, bool等）

2. **函数实例加载**:
   - 系统重启后，函数元数据可以从数据库加载
   - 但函数实例需要从代码中重新注册
   - 在 `load_and_run.go` 中，我们使用函数名称映射到实际函数

3. **参数传递**:
   - 参数通过 `map[string]string` 传递
   - 使用 `arg0`, `arg1` 等作为key，或使用类型名作为key

4. **异步执行**:
   - 函数被包装为异步执行，返回channel
   - 通过channel获取执行状态和结果
   - 支持长时间运行的IO操作

5. **IO操作模拟**:
   - 示例函数包含模拟的网络IO、文件IO等操作
   - 可以模拟真实的异步任务场景

## 最佳实践

### 1. 函数定义规范
- 第一个参数必须是 `context.Context`
- 最后一个返回值必须是 `error`
- 函数名称应该清晰、唯一
- 建议为函数添加描述信息

### 2. 程序启动流程
```go
// 1. 创建存储和注册中心
storage, _ := NewStorage("tasks.db")
registry := NewFunctionRegistry(storage)

// 2. 批量注册所有函数
registry.RegisterBatch(ctx, functions)

// 3. 恢复已存在的函数（如果程序重启）
registry.RestoreFunctions(ctx, funcMap)
```

### 3. 创建任务
- **推荐方式**: 使用 `NewTaskWithFunction`，通过函数引用创建，自动注册
- **备选方式**: 使用 `NewTask`，通过函数ID创建（函数需已注册）

### 4. 函数映射表
在程序重启时，需要提供函数名称到函数实例的映射表：
```go
funcMap := map[string]interface{}{
    "FunctionName": FunctionInstance,
    // ...
}
```

## 扩展建议

1. 支持更多参数类型（float, slice等）
2. 添加函数版本管理
3. 支持函数代码的序列化（如使用plugin机制）
4. 添加任务执行状态跟踪
5. 支持任务调度和定时执行
6. 添加函数自动发现机制（通过反射扫描包中的函数）

