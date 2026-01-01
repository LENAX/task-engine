# Task 选择策略对比分析

## 三种方案概述

### 方案1：现有方案（基于 candidateNodes 的手动检查）
- 使用 `candidateNodes` (sync.Map) 维护候选任务队列
- 手动遍历依赖名称，通过 `findTaskIDByName` 转换为 ID
- 在多个地方重复依赖检查逻辑

### 方案2：DAG 方案（完全基于 DAG）
- 直接使用 `dag.GetReadyTasks()` 获取入度为0的节点
- 利用 DAG 的入度信息自动计算就绪任务
- 不维护 candidateNodes，直接从 DAG 获取

### 方案3：混合方案（DAG + 优化）
- 使用 `dag.GetReadyTasks()` 获取就绪任务ID
- 使用 `workflow.TaskNameIndex` 进行 O(1) 的 name->id 转换
- 保留 candidateNodes 用于动态任务支持

---

## 详细对比

### 1. 性能分析

#### 方案1：现有方案

**时间复杂度：**
- `getAvailableTasks()`: O(C × D)
  - C = candidateNodes 中的任务数量
  - D = 平均每个任务的依赖数量
  - 每次调用需要遍历 candidateNodes，对每个任务检查所有依赖
- `findTaskIDByName()`: O(N)
  - N = workflow 中的总任务数
  - 通过遍历 `workflow.GetTasks()` 查找，最坏情况 O(N)
- 任务完成时检查下游: O(E × D)
  - E = 完成任务的出边数量（下游任务数）
  - D = 每个下游任务的平均依赖数

**空间复杂度：**
- candidateNodes: O(C)
- processedNodes: O(N)
- 总空间: O(N + C)

**实际性能问题：**
```go
// 问题1: findTaskIDByName 是 O(N) 查找
func findTaskIDByName(name string) string {
    for taskID, t := range m.workflow.GetTasks() {  // O(N)
        if t.GetName() == name {
            return taskID
        }
    }
    return ""
}

// 问题2: 重复的依赖检查逻辑
// 在 getAvailableTasks、createTaskCompleteHandler、RestoreFromBreakpoint 等多处重复
```

---

#### 方案2：DAG 方案

**时间复杂度：**
- `getAvailableTasks()`: O(R)
  - R = 当前就绪任务数量（入度为0的节点）
  - DAG 的 `GetReadyTasks()` 内部使用 `GetRoots()`，时间复杂度取决于根节点数量
- 任务完成时: O(1)
  - DAG 自动管理入度，无需手动检查下游
  - 下次调用 `GetReadyTasks()` 会自动包含新的就绪节点

**空间复杂度：**
- 不需要 candidateNodes: 节省 O(C) 空间
- processedNodes: O(N)
- DAG 内部结构: O(N + E)，E = 边数
- 总空间: O(N + E)

**性能优势：**
```go
// DAG 内部使用哈希表存储节点，GetRoots() 是 O(R)
func (d *DAG) GetReadyTasks() []string {
    roots := d.GetRoots()  // O(R)，R = 根节点数
    ready := make([]string, 0, len(roots))
    for id := range roots {
        ready = append(ready, id)
    }
    return ready
}
```

---

#### 方案3：混合方案

**时间复杂度：**
- `getAvailableTasks()`: O(R + R × V)
  - R = 就绪任务数量（从 DAG 获取）
  - V = 参数校验和 resultMapping 的开销（业务逻辑）
  - name->id 转换: O(1)（使用 TaskNameIndex）
- 任务完成时: O(1)
  - DAG 自动管理，但可能需要更新 candidateNodes（用于动态任务）

**空间复杂度：**
- candidateNodes: O(C)（可选，用于动态任务）
- processedNodes: O(N)
- DAG: O(N + E)
- 总空间: O(N + E + C)

**优化点：**
```go
// 使用 TaskNameIndex 进行 O(1) 查找
func findTaskIDByNameOptimized(name string) string {
    if taskIDValue, exists := m.workflow.TaskNameIndex.Load(name); exists {
        return taskIDValue.(string)  // O(1)
    }
    return ""
}
```

---

### 2. 代码复杂度对比

#### 方案1：现有方案

**代码行数：**
- `getAvailableTasks()`: ~50 行
- `createTaskCompleteHandler()` 中的依赖检查: ~30 行
- `RestoreFromBreakpoint()` 中的依赖检查: ~40 行
- `recoverPendingTasks()` 中的依赖检查: ~30 行
- **总计：~150 行重复的依赖检查逻辑**

**维护成本：**
- 高：依赖检查逻辑分散在多个地方
- 修改依赖检查逻辑需要同步更新多处
- 容易产生不一致的 bug

**示例：**
```go
// 在 getAvailableTasks 中
deps := t.GetDependencies()
allDepsProcessed := true
for _, depName := range deps {
    depTaskID := m.findTaskIDByName(depName)  // 重复逻辑
    if _, processed := m.processedNodes.Load(depTaskID); !processed {
        allDepsProcessed = false
        break
    }
}

// 在 createTaskCompleteHandler 中（几乎相同的代码）
for _, depName := range t.GetDependencies() {
    depTaskID := m.findTaskIDByName(depName)  // 重复逻辑
    if _, processed := m.processedNodes.Load(depTaskID); !processed {
        allDepsProcessed = false
        break
    }
}
```

---

#### 方案2：DAG 方案

**代码行数：**
- `getAvailableTasks()`: ~20 行（大幅简化）
- 任务完成时: 0 行（无需手动检查）
- **总计：~20 行**

**维护成本：**
- 低：逻辑集中在 DAG，依赖检查由 DAG 自动完成
- 修改只需更新 DAG 相关代码

**示例：**
```go
func getAvailableTasks() []workflow.Task {
    var available []workflow.Task
    
    // 直接从 DAG 获取就绪任务
    readyTaskIDs := m.dag.GetReadyTasks()  // 一行代码！
    
    for _, taskID := range readyTaskIDs {
        if _, processed := m.processedNodes.Load(taskID); processed {
            continue
        }
        if task, exists := m.workflow.GetTasks()[taskID]; exists {
            if err := m.validateAndMapParams(task, taskID); err != nil {
                continue
            }
            available = append(available, task)
        }
    }
    return available
}
```

---

#### 方案3：混合方案

**代码行数：**
- `getAvailableTasks()`: ~25 行
- 任务完成时: ~5 行（可选，用于动态任务支持）
- **总计：~30 行**

**维护成本：**
- 中：比方案1简单，比方案2稍复杂
- 需要同时维护 DAG 和 candidateNodes（如果使用）

---

### 3. 功能完整性对比

#### 方案1：现有方案

**支持的功能：**
- ✅ 静态任务依赖检查
- ✅ 动态任务支持（通过 candidateNodes）
- ✅ 参数校验和 resultMapping
- ✅ 任务恢复（recoverPendingTasks）

**存在的问题：**
- ❌ 依赖检查逻辑重复
- ❌ name->id 转换效率低（O(N)）
- ❌ 代码维护成本高

---

#### 方案2：DAG 方案

**支持的功能：**
- ✅ 静态任务依赖检查（自动）
- ✅ 参数校验和 resultMapping
- ⚠️ 动态任务支持（需要额外处理）
- ⚠️ 任务恢复（需要重新构建 DAG 或特殊处理）

**优势：**
- ✅ 代码简洁
- ✅ 性能最优
- ✅ 依赖检查自动完成

**潜在问题：**
- ⚠️ 动态添加任务时，DAG 需要更新（已有 `AddNode` 方法）
- ⚠️ 如果 DAG 不支持动态更新入度，可能需要重新构建

---

#### 方案3：混合方案

**支持的功能：**
- ✅ 静态任务依赖检查（通过 DAG）
- ✅ 动态任务支持（通过 candidateNodes 或 DAG.AddNode）
- ✅ 参数校验和 resultMapping
- ✅ 任务恢复（灵活处理）
- ✅ 优化的 name->id 转换（O(1)）

**优势：**
- ✅ 兼顾性能和灵活性
- ✅ 代码复杂度适中
- ✅ 支持所有现有功能

---

### 4. 实际场景性能测试

假设场景：
- 总任务数 N = 100
- 候选任务数 C = 10
- 平均依赖数 D = 3
- 就绪任务数 R = 5

#### 方案1：现有方案
```
getAvailableTasks() 调用：
- 遍历 candidateNodes: 10 次
- 每个任务检查依赖: 3 次
- findTaskIDByName: 3 × O(100) = 300 次比较
- 总操作数: ~330 次
```

#### 方案2：DAG 方案
```
getAvailableTasks() 调用：
- GetReadyTasks(): O(5) = 5 次操作
- 过滤已处理: 5 次
- 总操作数: ~10 次
```

#### 方案3：混合方案
```
getAvailableTasks() 调用：
- GetReadyTasks(): O(5) = 5 次操作
- TaskNameIndex.Load(): 如果需要转换，O(1) × 次数
- 过滤已处理: 5 次
- 总操作数: ~10-15 次
```

**性能提升：**
- 方案2 vs 方案1: **33倍** 性能提升
- 方案3 vs 方案1: **22-33倍** 性能提升

---

## 推荐方案

### 推荐：方案3（混合方案）

**理由：**
1. **性能优秀**：利用 DAG 的 O(R) 查找，比方案1快 20-30 倍
2. **代码简洁**：减少 80% 的重复代码
3. **功能完整**：支持所有现有功能，包括动态任务
4. **易于维护**：逻辑集中，依赖检查由 DAG 自动完成
5. **向后兼容**：可以逐步迁移，不影响现有功能

### 实施建议

**阶段1：优化 name->id 转换**
```go
// 替换 findTaskIDByName，使用 TaskNameIndex
func (m *WorkflowInstanceManager) findTaskIDByName(name string) string {
    if taskIDValue, exists := m.workflow.TaskNameIndex.Load(name); exists {
        return taskIDValue.(string)  // O(1)
    }
    return ""
}
```

**阶段2：使用 DAG 获取就绪任务**
```go
func (m *WorkflowInstanceManager) getAvailableTasks() []workflow.Task {
    var available []workflow.Task
    
    // 使用 DAG 获取就绪任务
    readyTaskIDs := m.dag.GetReadyTasks()
    
    for _, taskID := range readyTaskIDs {
        // 检查是否已处理
        if _, processed := m.processedNodes.Load(taskID); processed {
            continue
        }
        
        // 获取任务
        task, exists := m.workflow.GetTasks()[taskID]
        if !exists {
            continue
        }
        
        // 参数校验和 resultMapping（业务逻辑）
        if err := m.validateAndMapParams(task, taskID); err != nil {
            log.Printf("参数校验失败: TaskID=%s, Error=%v", taskID, err)
            continue
        }
        
        available = append(available, task)
    }
    
    return available
}
```

**阶段3：简化任务完成处理**
```go
// 任务完成时，DAG 自动更新入度
// 下次 getAvailableTasks() 会自动包含新的就绪节点
// 但仍需要处理动态任务的情况
```

---

## 总结对比表

| 维度 | 方案1（现有） | 方案2（DAG） | 方案3（混合） |
|------|--------------|-------------|--------------|
| **性能** | O(C × D × N) | O(R) | O(R) |
| **代码行数** | ~150 行 | ~20 行 | ~30 行 |
| **维护成本** | 高 | 低 | 中 |
| **功能完整性** | ✅ 完整 | ⚠️ 需处理动态任务 | ✅ 完整 |
| **向后兼容** | ✅ | ⚠️ 需重构 | ✅ |
| **推荐度** | ⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

**最终建议：采用方案3（混合方案）**

