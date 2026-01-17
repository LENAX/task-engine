# 股票数据采集流程示例

这是一个贴近实际业务的示例，展示了如何使用 task-engine 实现一个完整的股票数据采集流程。

## 业务场景

该示例模拟了一个典型的金融数据采集场景：

1. **获取股票列表**：从数据源获取股票代码列表
2. **动态生成子任务**：为每个股票代码生成一个日线数据采集子任务（使用模板任务）
3. **数据汇总**：等待所有子任务完成后，进行数据汇总

## 核心特性

### 1. 模板任务（Template Task）

模板任务是一种特殊的任务类型，它本身不执行具体业务逻辑，而是在其 Job Function 中动态生成子任务。

```go
// 模板任务：生成日线数据子任务
task2, err := builder.NewTaskBuilder("生成日线数据子任务", "为每个股票生成日线数据子任务", registry).
    WithJobFunction("GenerateDailyDataSubTasks", nil).
    WithDependency("获取股票列表").
    WithTemplate(true). // 标记为模板任务
    Build()
```

### 2. 动态子任务生成

在模板任务的 Job Function 中，可以从上游任务的结果中提取数据，然后为每个数据项生成一个子任务：

```go
func GenerateDailyDataSubTasks(tc *task.TaskContext) (interface{}, error) {
    // 1. 获取Engine依赖
    eng := tc.GetDependency("Engine").(*engine.Engine)
    
    // 2. 从上游任务结果中提取股票代码列表
    stockCodes := extractStockCodesFromUpstream(tc)
    
    // 3. 为每个股票代码生成子任务
    for _, stockCode := range stockCodes {
        subTask := builder.NewTaskBuilder(...).Build()
        eng.AddSubTaskToInstance(ctx, workflowInstanceID, subTask, parentTaskID)
    }
    
    return result, nil
}
```

### 3. 任务依赖关系

- **Level 0**: 获取股票列表（无依赖）
- **Level 1**: 生成日线数据子任务（依赖：获取股票列表）
- **Level 2**: 动态生成的子任务（依赖：生成日线数据子任务）
- **Level 3**: 数据汇总（依赖：生成日线数据子任务，实际会等待所有子任务完成）

## 运行示例

### 前置要求

- Go 1.21+
- 已安装 task-engine 依赖

### 运行步骤

```bash
cd task-engine/examples/example_workflow
go run main.go
```

### 预期输出

```
========== 股票数据采集流程示例 ==========
📁 数据库路径: /tmp/task-engine-example/...
✅ Engine已启动
✅ 函数注册完成
✅ Workflow构建完成
✅ Workflow已提交，实例ID: ...
⏳ 等待Workflow执行完成...
📊 当前状态: Running
...
✅ Workflow执行完成，状态: Success，耗时: ...
========== 执行结果 ==========
Workflow状态: Success
✅ 示例执行完成
```

## 代码结构

```
main.go
├── Job Functions（业务函数）
│   ├── FetchStockList          # 获取股票列表
│   ├── GenerateDailyDataSubTasks # 生成子任务（模板任务使用）
│   ├── FetchDailyData           # 获取单个股票的日线数据（子任务使用）
│   └── AggregateData            # 数据汇总
├── Task Handlers（任务处理器）
│   ├── LogSuccess               # 成功日志
│   └── LogError                 # 错误日志
└── main                         # 主函数（构建和运行Workflow）
```

## 关键概念

### 1. 模板任务 vs 普通任务

- **普通任务**：直接执行业务逻辑，返回结果
- **模板任务**：不执行业务逻辑，而是在 Job Function 中生成子任务

### 2. 子任务生成时机

模板任务的 Job Function 执行时，会：
1. 从上游任务的结果中提取数据
2. 为每个数据项创建一个子任务
3. 通过 `eng.AddSubTaskToInstance()` 将子任务添加到 Workflow 实例中

### 3. 依赖关系处理

- 模板任务可以依赖普通任务，获取上游任务的结果
- 子任务会自动继承模板任务的依赖关系
- 下游任务依赖模板任务时，会等待所有子任务完成

## 扩展建议

### 1. 添加更多数据源

可以扩展 `FetchStockList` 函数，从多个数据源获取股票列表：

```go
func FetchStockList(tc *task.TaskContext) (interface{}, error) {
    // 从多个数据源获取
    codes1 := fetchFromSource1()
    codes2 := fetchFromSource2()
    // 合并去重
    return mergedCodes, nil
}
```

### 2. 添加错误处理

可以为子任务添加重试机制：

```go
subTask, err := builder.NewTaskBuilder(...).
    WithRetryCount(3). // 失败后重试3次
    Build()
```

### 3. 添加数据验证

在数据汇总前，可以添加数据验证任务：

```go
task3 := builder.NewTaskBuilder("数据验证", "验证数据完整性", registry).
    WithJobFunction("ValidateData", nil).
    WithDependency("生成日线数据子任务").
    Build()

task4 := builder.NewTaskBuilder("数据汇总", "汇总数据", registry).
    WithJobFunction("AggregateData", nil).
    WithDependency("数据验证"). // 依赖验证任务
    Build()
```

### 4. 添加并发控制

可以通过 Engine 的并发数控制同时执行的子任务数量：

```go
// 创建Engine时设置并发数
eng, err := engine.NewEngine(50, 60, ...) // 50个并发worker
```

## 参考

- [动态添加子任务的设计文档](../../doc/dev/动态添加子任务的设计.md)
- [E2E测试示例](../../test/e2e/e2e_test.go) - 更复杂的业务场景示例
