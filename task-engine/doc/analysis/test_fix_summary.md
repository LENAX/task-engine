# InstanceManagerV2 测试修复总结

## 问题描述

`TestInstanceManagerV2_MultipleTemplateTasks` 测试会卡住，无法完成。

## 根本原因分析

### 1. 模板任务计数竞态条件

**问题**：
- 模板任务在 `submitBatch` 中被处理，设置为 SUCCESS 并调用 handler
- handler 在 goroutine 中异步执行，调用 `AtomicAddSubTasks` 添加子任务
- 同时，`createTaskCompleteHandler` 发送 `TaskStatusEvent` 到 `taskStatusChan`
- `handleTaskSuccess` 可能在 handler 中的 `AtomicAddSubTasks` 执行之前就被调用
- `handleTaskSuccess` 检查时发现没有子任务，错误地减少了模板任务计数
- 导致层级无法推进，测试卡住

### 2. 测试缺少错误处理

**问题**：
- handler 中的错误被忽略（`_ = instanceManager.AtomicAddSubTasks(...)`）
- 测试超时时间过长（1分钟），难以快速发现问题
- 缺少详细的调试信息

## 修复方案

### 1. 添加处理标记机制

在 `handleAtomicAddSubTasks` 函数开始时，如果是模板任务，立即设置处理标记：

```go
// 如果是模板任务，立即设置处理标记（避免 handleTaskSuccess 误判）
if parentTask.IsTemplate() {
    processingKey := fmt.Sprintf("%s:subtasks_processing", parentTaskID)
    m.contextData.Store(processingKey, true)
    defer func() {
        // 处理完成后删除标记（只有在没有子任务等待依赖时才删除）
        subTasksKey := fmt.Sprintf("%s:subtasks", parentTaskID)
        if _, hasWaitingSubTasks := m.contextData.Load(subTasksKey); !hasWaitingSubTasks {
            m.contextData.Delete(processingKey)
        }
    }()
}
```

### 2. 改进 handleTaskSuccess 中的检查逻辑

在 `handleTaskSuccess` 中，检查处理标记和 runtimeTasks：

```go
// 检查是否有待处理的子任务（通过contextData检查）
subTasksKey := fmt.Sprintf("%s:subtasks", event.TaskID)
// 检查是否已经标记为有子任务正在处理
processingKey := fmt.Sprintf("%s:subtasks_processing", event.TaskID)

// 如果既没有待处理的子任务，也没有正在处理的标记，说明模板任务没有生成子任务
_, hasSubTasks := m.contextData.Load(subTasksKey)
_, isProcessing := m.contextData.Load(processingKey)

// 还检查 runtimeTasks 中是否有子任务
hasRuntimeSubTasks := false
m.runtimeTasks.Range(func(key, value interface{}) bool {
    if task, ok := value.(workflow.Task); ok {
        if task.IsSubTask() {
            hasRuntimeSubTasks = true
            return false
        }
    }
    return true
})

if !hasSubTasks && !isProcessing && !hasRuntimeSubTasks {
    // 确实没有子任务，才减少计数
    // ...
}
```

### 3. 加强测试逻辑

1. **添加错误处理**：handler 中的错误不再被忽略，记录日志
2. **减少超时时间**：从 1 分钟减少到 30 秒，更快发现问题
3. **添加调试信息**：
   - 定期输出任务状态和任务数
   - 检测是否卡住（状态和任务数都没有变化）
   - 超时时输出详细信息

## 修复的文件

1. `task-engine/pkg/core/engine/instance_manager_v2.go`
   - 修复模板任务计数竞态条件
   - 添加处理标记机制

2. `task-engine/test/integration/instance_manager_v2_complex_test.go`
   - 加强测试逻辑
   - 添加错误处理和调试信息

## 待进一步调试

测试仍然可能卡住，需要进一步调试：

1. **检查子任务是否被正确添加**：查看日志中是否有 "原子性地批量添加" 的消息
2. **检查模板任务计数**：查看日志中模板任务计数的变化
3. **检查层级推进**：查看日志中是否有 "currentLevel 从 X 推进到 Y" 的消息
4. **检查任务执行**：查看日志中是否有子任务的执行记录

## 建议的调试步骤

1. 运行测试并查看完整日志：
   ```bash
   go test -v ./test/integration -run "TestInstanceManagerV2_MultipleTemplateTasks$" 2>&1 | tee test.log
   ```

2. 分析日志，查找：
   - "templateHandler 开始执行"
   - "原子性地批量添加"
   - "templateTaskCount 从 X 减少到 Y"
   - "currentLevel 从 X 推进到 Y"

3. 如果测试仍然卡住，可能需要：
   - 检查 `tryBatchAddSubTasks` 的逻辑
   - 检查子任务依赖检查的逻辑
   - 添加更多日志输出

