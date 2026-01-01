# 线程安全分析报告

## 概述

本文档分析了 task-engine 项目的线程安全问题，识别了潜在的竞态条件和需要加强保护的区域。

## 1. Workflow 结构体 (`pkg/core/workflow/workflow.go`)

### ✅ 已保护的部分

1. **Tasks, TaskNameIndex, Dependencies**
   - 使用 `sync.Map` 实现线程安全
   - 所有操作方法（AddTask, UpdateTask, DeleteTask等）都正确使用了 sync.Map 的原子操作

2. **GetTasks(), GetDependencies()**
   - 返回副本，避免外部修改影响内部状态

### ⚠️ 潜在问题

1. **Status 字段未保护**
   ```go
   type Workflow struct {
       Status string `json:"status"` // ENABLED/DISABLED
       // ...
   }
   ```
   - **问题**: `Status` 字段是普通字符串，没有锁保护
   - **风险**: 如果多个 goroutine 同时修改 Status，可能导致数据竞争
   - **建议**: 
     - 如果 Status 需要并发修改，添加 `sync.RWMutex` 保护
     - 或者将 Status 改为只读字段（仅在创建时设置）

2. **未使用的锁字段**
   ```go
   tasksMu sync.RWMutex  // 用于序列化访问Tasks（如果需要批量操作）
   depsMu  sync.RWMutex  // 用于序列化访问Dependencies（如果需要批量操作）
   ```
   - **问题**: 定义了但从未使用
   - **建议**: 
     - 如果不需要批量操作，删除这些字段
     - 如果需要批量操作，实现相应的方法并使用这些锁

3. **Params 字段未保护**
   ```go
   Params map[string]string `json:"params"`
   ```
   - **问题**: 普通 map，没有并发保护
   - **风险**: 如果多个 goroutine 同时读写 Params，可能导致 panic
   - **建议**: 
     - 如果 Params 需要并发访问，使用 `sync.RWMutex` 保护
     - 或者提供线程安全的访问方法

## 2. Engine 结构体 (`pkg/core/engine/engine.go`)

### ✅ 已保护的部分

1. **managers 和 controllers 映射**
   ```go
   managers    map[string]*WorkflowInstanceManager
   controllers map[string]workflow.WorkflowController
   mu          sync.RWMutex
   ```
   - 所有访问都正确使用了 `mu` 锁保护

2. **running 字段**
   - 使用 `mu` 锁保护，访问时都加锁

### ⚠️ 潜在问题

1. **jobRegistry, callbackRegistry, serviceRegistry 未保护**
   ```go
   jobRegistry      map[string]interface{} // Job函数注册表
   callbackRegistry map[string]interface{} // Callback函数注册表
   serviceRegistry  map[string]interface{} // 服务依赖注册表
   ```
   - **问题**: 这些 map 没有锁保护
   - **风险**: 如果多个 goroutine 同时访问这些注册表，可能导致 panic
   - **建议**: 
     - 使用 `sync.RWMutex` 保护这些 map
     - 或者使用 `sync.Map` 替代普通 map

2. **cfg 字段未保护**
   ```go
   cfg *config.EngineConfig
   ```
   - **问题**: 如果 cfg 在运行时被修改，没有保护
   - **建议**: 
     - 如果 cfg 是只读的，可以不加锁
     - 如果需要修改，添加锁保护

## 3. WorkflowInstanceManager 结构体 (`pkg/core/engine/instance_manager.go`)

### ✅ 已保护的部分

1. **processedNodes, candidateNodes**
   - 使用 `sync.Map` 实现线程安全

2. **instance.Status**
   - 使用 `mu` 锁保护所有状态修改

3. **控制信号通道**
   - 使用带缓冲的 channel，线程安全

### ⚠️ 严重问题

1. **contextData 未保护** ⚠️ **高优先级**
   ```go
   contextData map[string]interface{} // Task间传递的数据
   ```
   - **问题**: 普通 map，没有锁保护，但被多个 goroutine 并发访问
   - **风险**: 
     - 在 `validateAndMapParams()` 中读取：`m.contextData[depTaskID]`
     - 在 `createTaskCompleteHandler()` 中写入：`m.contextData[taskID] = result.Data`
     - 在 `createBreakpoint()` 中读取：`ContextData: m.contextData`
     - 可能导致 panic 或数据竞争
   - **建议**: 
     ```go
     // 方案1: 使用 sync.RWMutex 保护
     type WorkflowInstanceManager struct {
         // ...
         contextDataMu sync.RWMutex
         contextData   map[string]interface{}
     }
     
     // 读取时
     m.contextDataMu.RLock()
     value := m.contextData[key]
     m.contextDataMu.RUnlock()
     
     // 写入时
     m.contextDataMu.Lock()
     m.contextData[key] = value
     m.contextDataMu.Unlock()
     
     // 方案2: 使用 sync.Map
     contextData sync.Map // key: string, value: interface{}
     ```

2. **workflow 字段未保护**
   ```go
   workflow *workflow.Workflow
   ```
   - **问题**: 如果 workflow 在运行时被修改（如添加子任务），没有保护
   - **风险**: 在 `AddSubTask()` 中会修改 workflow，可能与读取操作冲突
   - **建议**: 
     - Workflow 本身使用 sync.Map，但需要确保 AddSubTask 操作的原子性
     - 在 Manager 中添加锁保护 workflow 的访问

3. **findTaskIDByName() 方法**
   ```go
   func (m *WorkflowInstanceManager) findTaskIDByName(name string) string {
       for taskID, t := range m.workflow.GetTasks() {
           if t.GetName() == name {
               return taskID
           }
       }
       return ""
   }
   ```
   - **问题**: 遍历 workflow.GetTasks() 时，如果 workflow 被并发修改，可能获取到不一致的快照
   - **建议**: 
     - 使用 Workflow 的 `GetTaskByName()` 方法（已优化，使用 TaskNameIndex）
     - 或者添加锁保护

## 4. Task 结构体 (`pkg/core/task/task.go`)

### ⚠️ 严重问题

1. **所有字段都是公开的，没有保护**
   ```go
   type Task struct {
       ID             string
       Name           string
       Status         string
       Params         map[string]any
       // ...
   }
   ```
   - **问题**: 
     - `Status` 字段可能被多个 goroutine 并发修改
     - `Params` map 可能被并发读写
   - **风险**: 
     - 在 Executor 中，Task 可能被多个 goroutine 访问
     - 状态更新可能导致数据竞争
   - **建议**: 
     - 将 Task 字段改为私有，提供线程安全的访问方法
     - 或者为 Task 添加 `sync.RWMutex` 保护
     - 对于 Status，使用原子操作或锁保护

2. **UpdateParams() 方法**
   ```go
   func (t *Task) UpdateParams(newParams map[string]any) error {
       // 直接修改 t.Params，没有锁保护
       for k, v := range newParams {
           t.Params[k] = v
       }
   }
   ```
   - **问题**: 如果 Task 正在执行时调用 UpdateParams，可能导致数据竞争
   - **建议**: 添加锁保护

## 5. Executor 结构体 (`pkg/core/executor/executor.go`)

### ✅ 已保护的部分

1. **domainPools 映射**
   - 使用 `mu` 锁保护

2. **running 字段**
   - 使用 `mu` 锁保护

3. **任务队列**
   - 使用 channel，线程安全

### ⚠️ 潜在问题

1. **domainPool.current 字段**
   ```go
   type domainPool struct {
       current int
       mu      sync.RWMutex
   }
   ```
   - **问题**: 虽然定义了 `mu`，但在修改 `current` 时使用了锁，但读取时可能没有加锁
   - **建议**: 确保所有对 `current` 的访问都加锁

## 6. FunctionRegistry 结构体 (`pkg/core/task/registry.go`)

### ✅ 已保护的部分

1. **所有 map 字段**
   - 使用 `mu` 锁保护所有访问

2. **Register, Get, GetByName 等方法**
   - 都正确使用了锁保护

## 7. 其他潜在问题

### 1. WorkflowInstance 结构体

```go
type WorkflowInstance struct {
    Status       string
    Breakpoint   *BreakpointData
    // ...
}
```

- **问题**: 如果 WorkflowInstance 被多个 goroutine 访问，Status 字段没有保护
- **建议**: 
  - 如果 WorkflowInstance 只在 Manager 内部使用，由 Manager 的锁保护即可
  - 如果对外暴露，需要添加锁保护

### 2. 通道操作

- **问题**: 某些 channel 操作没有检查 channel 是否已关闭
- **建议**: 使用 `select` 语句和 `ctx.Done()` 检查

## 优先级修复建议

### 🔴 高优先级（可能导致 panic 或数据竞争）

1. **WorkflowInstanceManager.contextData**
   - 立即修复：添加锁保护或使用 sync.Map

2. **Task 结构体的并发访问**
   - 添加锁保护 Status 和 Params 字段

3. **Engine 的注册表（jobRegistry, callbackRegistry, serviceRegistry）**
   - 添加锁保护

### 🟡 中优先级（可能导致数据不一致）

1. **Workflow.Status 和 Params**
   - 如果需要在运行时修改，添加锁保护

2. **WorkflowInstanceManager.workflow**
   - 确保 AddSubTask 操作的原子性

3. **findTaskIDByName() 方法**
   - 使用 Workflow.GetTaskByName() 替代

### 🟢 低优先级（代码清理）

1. **删除未使用的锁字段**
   - Workflow.tasksMu 和 depsMu

2. **代码审查**
   - 检查所有 map 的并发访问
   - 确保所有 channel 操作都有适当的错误处理

## 测试建议

1. **并发测试**
   - 使用 `go test -race` 运行所有测试
   - 添加专门的并发测试用例

2. **压力测试**
   - 模拟高并发场景
   - 测试 AddSubTask 的并发调用

3. **数据竞争检测**
   - 使用 Go 的 race detector
   - 在 CI/CD 中集成 race detector

## 总结

项目整体线程安全设计较好，主要使用了：
- `sync.Map` 保护并发访问的映射
- `sync.RWMutex` 保护共享状态
- Channel 进行 goroutine 间通信

但仍有几个关键问题需要修复：
1. **contextData 未保护**（最严重）
2. **Task 结构体字段未保护**
3. **Engine 注册表未保护**

建议优先修复高优先级问题，然后进行全面的并发测试。

