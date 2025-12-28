# 用户使用指南

本指南介绍如何在您的项目中使用任务函数持久化和加载功能。

## 快速开始

### 1. 定义您的函数

在您的package中定义任务函数（例如 `my_functions.go`）：

```go
package main

import (
    "context"
    "fmt"
)

// 定义您的任务函数
// 要求：第一个参数必须是 context.Context，最后一个返回值必须是 error
func ProcessOrder(ctx context.Context, orderID string, amount int) (string, error) {
    // 您的业务逻辑
    result := fmt.Sprintf("处理订单: %s, 金额: %d", orderID, amount)
    return result, nil
}

func SendEmail(ctx context.Context, to string, subject string) error {
    // 发送邮件逻辑
    fmt.Printf("发送邮件到: %s, 主题: %s\n", to, subject)
    return nil
}
```

### 2. 在main.go中初始化

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
    
    // 2. 批量注册所有函数（程序启动时执行一次）
    functions := []FunctionDef{
        {Name: "ProcessOrder", Description: "处理订单", Function: ProcessOrder},
        {Name: "SendEmail", Description: "发送邮件", Function: SendEmail},
        // 添加您的其他函数...
    }
    
    if err := registry.RegisterBatch(ctx, functions); err != nil {
        log.Fatalf("批量注册函数失败: %v", err)
    }
    log.Println("✓ 函数注册完成")
    
    // 3. 恢复已存在的函数（如果程序重启，从数据库恢复）
    funcMap := map[string]interface{}{
        "ProcessOrder": ProcessOrder,
        "SendEmail":    SendEmail,
        // 添加您的其他函数映射...
    }
    
    if err := registry.RestoreFunctions(ctx, funcMap); err != nil {
        log.Fatalf("恢复函数失败: %v", err)
    }
    log.Println("✓ 函数恢复完成")
    
    // 现在可以使用registry和storage了...
}
```

### 3. 创建任务

有两种方式创建任务：

#### 方式1: 通过函数引用创建（推荐）

```go
// 直接传入函数引用，系统会自动注册（如果未注册）
task, err := NewTaskWithFunction(ctx, registry,
    "处理订单任务",           // 任务名称
    "处理用户订单",           // 任务描述
    ProcessOrder,            // 函数引用
    "ProcessOrder",          // 函数名称
    "处理订单",              // 函数描述
)
if err != nil {
    log.Fatalf("创建任务失败: %v", err)
}

// 设置任务参数
task.Params["arg0"] = "ORDER-12345"  // 第一个参数（orderID）
task.Params["arg1"] = "1000"         // 第二个参数（amount）

// 保存任务
if err := storage.SaveTask(ctx, task); err != nil {
    log.Fatalf("保存任务失败: %v", err)
}
```

#### 方式2: 通过函数ID创建

```go
// 先获取函数ID
funcID := registry.GetIDByName("ProcessOrder")
if funcID == "" {
    log.Fatal("函数未找到")
}

// 创建任务
task := NewTask("处理订单任务", "处理用户订单", funcID)
task.Params["arg0"] = "ORDER-12345"
task.Params["arg1"] = "1000"

// 保存任务
if err := storage.SaveTask(ctx, task); err != nil {
    log.Fatalf("保存任务失败: %v", err)
}
```

### 4. 执行任务

```go
// 从数据库加载任务
tasks, err := storage.ListAllTasks(ctx)
if err != nil {
    log.Fatalf("加载任务失败: %v", err)
}

// 执行任务
for _, task := range tasks {
    // 获取函数
    fn := registry.Get(task.FuncID)
    if fn == nil {
        log.Printf("任务 %s 的函数未找到", task.Name)
        continue
    }
    
    // 执行任务（异步，通过channel获取结果）
    stateCh := fn(ctx, task.Params)
    state := <-stateCh
    
    if state.Status == "Success" {
        log.Printf("✓ 任务 %s 执行成功: %v", task.Name, state.Data)
    } else {
        log.Printf("❌ 任务 %s 执行失败: %v", task.Name, state.Error)
    }
}
```

## 完整示例

参考 `example_usage.go` 文件，包含完整的使用示例。

## 关键点

1. **函数签名要求**：
   - 第一个参数必须是 `context.Context`
   - 最后一个返回值必须是 `error`
   - 支持基本类型参数（string, int, bool等）

2. **参数传递**：
   - 使用 `arg0`, `arg1` 等作为key传递参数
   - 参数值都是字符串类型，系统会自动转换

3. **程序启动流程**：
   - 创建存储和注册中心
   - 批量注册所有函数
   - 恢复已存在的函数（从数据库）

4. **函数映射表**：
   - 在程序重启时，需要提供函数名称到函数实例的映射
   - 这样系统才能从数据库恢复函数实例

5. **任务创建**：
   - 推荐使用 `NewTaskWithFunction`，自动处理函数注册
   - 也可以使用 `NewTask`，但需要先注册函数

## 注意事项

- 函数名称必须唯一
- 函数实例必须在程序启动时注册
- 程序重启后，函数实例需要重新注册（通过 `RestoreFunctions`）
- 参数类型转换支持：string, int, uint, float, bool

