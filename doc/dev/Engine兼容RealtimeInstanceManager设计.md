# Engine 兼容 RealtimeInstanceManager 设计文档

## 一、设计目标

让 Engine 能够根据 Workflow 类型自动选择合适的 InstanceManager：
- **批处理 Workflow** → 使用 `WorkflowInstanceManagerV2`
- **实时 Workflow** → 使用 `RealtimeInstanceManager`

同时保持：
- ✅ 接口统一：Engine 通过 `types.WorkflowInstanceManager` 接口使用
- ✅ 向后兼容：现有批处理 Workflow 不受影响
- ✅ 自动选择：根据 Workflow 类型自动选择 Manager
- ✅ 类型安全：提供类型断言访问实时特定功能

---

## 二、设计方案

### 2.1 方案概述

采用**策略模式 + 类型检测**的方式：

1. **类型检测**：在 `SubmitWorkflow` 时检测 Workflow 类型
2. **策略选择**：根据类型选择对应的 Manager 创建策略
3. **统一接口**：所有 Manager 都实现 `types.WorkflowInstanceManager` 接口
4. **扩展访问**：通过类型断言访问实时特定功能

### 2.2 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                         Engine                                │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  SubmitWorkflow(wf)                                          │
│    │                                                          │
│    ├─→ 检测 Workflow 类型                                     │
│    │   ├─→ 是 RealtimeWorkflow?                              │
│    │   │   └─→ 创建 RealtimeInstanceManager                  │
│    │   └─→ 是普通 Workflow?                                   │
│    │       └─→ 创建 WorkflowInstanceManagerV2                │
│    │                                                          │
│    └─→ 统一通过 types.WorkflowInstanceManager 接口使用       │
│                                                               │
│  managers map[string]types.WorkflowInstanceManager           │
│    ├─→ instanceID1 → WorkflowInstanceManagerV2              │
│    ├─→ instanceID2 → RealtimeInstanceManager                │
│    └─→ instanceID3 → WorkflowInstanceManagerV2              │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

---

## 三、实现方案

### 3.1 核心设计：Workflow 工作模式字段

**设计决策**：在 `Workflow` 结构体中添加 `ExecutionMode` 字段，用于指定工作模式。

#### 3.1.1 字段定义

```go
// pkg/core/workflow/workflow.go

// ExecutionMode 工作流执行模式
type ExecutionMode string

const (
    ExecutionModeBatch     ExecutionMode = "batch"     // 批处理模式（默认）
    ExecutionModeStreaming ExecutionMode = "streaming"  // 流处理模式（实时）
)

// Workflow 添加执行模式字段
type Workflow struct {
    // ... 现有字段 ...
    
    // 执行模式：指定 Workflow 的工作模式
    // - "batch": 批处理模式，使用 WorkflowInstanceManagerV2
    // - "streaming": 流处理模式，使用 RealtimeInstanceManager
    // 默认值为 "batch"，保证向后兼容
    ExecutionMode ExecutionMode `json:"execution_mode"`
    
    // 保护 ExecutionMode 字段的锁（如果需要）
    executionModeMu sync.RWMutex `json:"-"`
}
```

#### 3.1.2 字段特性

| 特性 | 说明 |
|------|------|
| **字段名** | `ExecutionMode` |
| **类型** | `ExecutionMode` (string 类型别名) |
| **可选值** | `"batch"` 或 `"streaming"` |
| **默认值** | `"batch"`（批处理模式） |
| **是否必填** | 否（为空时使用默认值） |
| **是否持久化** | 是（保存到数据库） |
| **线程安全** | 是（通过 `executionModeMu` 保护） |

#### 3.1.3 字段方法

```go
// GetExecutionMode 获取执行模式（线程安全）
func (w *Workflow) GetExecutionMode() ExecutionMode {
    w.executionModeMu.RLock()
    defer w.executionModeMu.RUnlock()
    
    if w.ExecutionMode == "" {
        return ExecutionModeBatch  // 默认批处理模式
    }
    return w.ExecutionMode
}

// SetExecutionMode 设置执行模式（线程安全，带校验）
func (w *Workflow) SetExecutionMode(mode ExecutionMode) error {
    // 校验模式值
    if err := validateExecutionMode(mode); err != nil {
        return err
    }
    
    w.executionModeMu.Lock()
    defer w.executionModeMu.Unlock()
    w.ExecutionMode = mode
    return nil
}

// validateExecutionMode 校验执行模式值
func validateExecutionMode(mode ExecutionMode) error {
    switch mode {
    case ExecutionModeBatch, ExecutionModeStreaming:
        return nil
    case "":
        return nil  // 空值允许，会使用默认值
    default:
        return fmt.Errorf("无效的执行模式: %s，支持的模式: batch, streaming", mode)
    }
}
```

#### 3.1.4 初始化

```go
// NewWorkflow 创建Workflow实例（更新版）
func NewWorkflow(name, desc string) *Workflow {
    wf := &Workflow{
        ID:                    uuid.NewString(),
        Name:                  name,
        Description:           desc,
        status:                "ENABLED",
        CreateTime:            time.Now(),
        Tasks:                 sync.Map{},
        TaskNameIndex:         sync.Map{},
        Dependencies:          sync.Map{},
        SubTaskErrorTolerance: 0.0,
        Transactional:         false,
        TransactionMode:       "",
        MaxConcurrentTask:     10,
        CronExpr:              "",
        CronEnabled:           false,
        ExecutionMode:         ExecutionModeBatch,  // 默认批处理模式
    }
    return wf
}
```

### 3.2 校验逻辑设计

#### 3.2.1 Workflow 校验

在 `Workflow.Validate()` 方法中添加执行模式校验：

```go
// Validate 校验Workflow的合法性（更新版）
func (w *Workflow) Validate() error {
    // ... 现有校验逻辑 ...
    
    // 校验执行模式
    if err := validateExecutionMode(w.ExecutionMode); err != nil {
        return fmt.Errorf("执行模式校验失败: %w", err)
    }
    
    // 执行模式与任务类型的兼容性校验
    if w.ExecutionMode == ExecutionModeStreaming {
        // 流处理模式的特殊校验
        // 例如：检查是否有持续运行的任务
        // 这个校验可以在 Engine 层面进行，也可以在这里进行
    }
    
    return nil
}
```

#### 3.2.2 Engine 层面的校验

在 `Engine.SubmitWorkflow()` 中添加校验：

```go
// SubmitWorkflow 提交Workflow并创建WorkflowInstance（更新版）
func (e *Engine) SubmitWorkflow(ctx context.Context, wf *workflow.Workflow) (workflow.WorkflowController, error) {
    // ... 现有代码 ...
    
    // 校验 Workflow 合法性（包含执行模式校验）
    if err := wf.Validate(); err != nil {
        return nil, fmt.Errorf("Workflow校验失败: %w", err)
    }
    
    // 执行模式与任务类型的兼容性校验
    executionMode := wf.GetExecutionMode()
    if executionMode == ExecutionModeStreaming {
        // 流处理模式需要检查是否有实时任务
        // 如果没有实时任务，可以警告或报错
        if err := e.validateStreamingWorkflow(wf); err != nil {
            return nil, fmt.Errorf("流处理模式校验失败: %w", err)
        }
    }
    
    // ... 其余代码 ...
}

// validateStreamingWorkflow 校验流处理 Workflow
func (e *Engine) validateStreamingWorkflow(wf *workflow.Workflow) error {
    // 检查是否有持续运行的任务
    // 这个校验可以根据实际需求调整
    // 例如：检查是否有 ExecutionMode 为 "continuous" 的 Task
    
    // 示例：检查是否有实时任务（如果有 RealtimeWorkflow 类型）
    // 这里可以根据实际设计调整
    
    return nil
}
```

### 3.3 Engine 修改方案

#### 3.3.1 根据执行模式选择 Manager

```go
// pkg/core/engine/engine.go

// SubmitWorkflow 提交Workflow并创建WorkflowInstance
func (e *Engine) SubmitWorkflow(ctx context.Context, wf *workflow.Workflow) (workflow.WorkflowController, error) {
    if !e.running {
        return nil, logError("engine_not_running", "引擎未启动")
    }

    // 校验Workflow合法性
    if err := wf.Validate(); err != nil {
        return nil, fmt.Errorf("Workflow校验失败: %w", err)
    }

    var instance *workflow.WorkflowInstance

    // 优先使用聚合Repository（事务性保存Workflow和创建Instance）
    if e.aggregateRepo != nil {
        // 先保存Workflow定义
        if err := e.aggregateRepo.SaveWorkflow(ctx, wf); err != nil {
            return nil, fmt.Errorf("保存Workflow失败: %w", err)
        }
        // 启动Workflow（创建Instance和TaskInstance）
        var err error
        instance, err = e.aggregateRepo.StartWorkflow(ctx, wf)
        if err != nil {
            return nil, fmt.Errorf("启动Workflow失败: %w", err)
        }
    } else {
        // 使用旧的分离Repository
        instance = workflow.NewWorkflowInstance(wf.GetID())
        instance.Status = "Ready"

        // 持久化WorkflowInstance
        if e.workflowInstanceRepo != nil {
            if err := e.workflowInstanceRepo.Save(ctx, instance); err != nil {
                return nil, fmt.Errorf("保存WorkflowInstance失败: %w", err)
            }
        }

        // 在事务中同时保存Workflow模板和所有预定义的Task
        if e.workflowRepo != nil {
            if err := e.workflowRepo.SaveWithTasks(ctx, wf, instance.ID, e.taskRepo, e.registry); err != nil {
                return nil, fmt.Errorf("保存Workflow和Task失败: %w", err)
            }
        }
    }

    // ============================================
    // 根据 Workflow 执行模式选择 InstanceManager
    // ============================================
    var manager types.WorkflowInstanceManager
    var managerErr error
    
    executionMode := wf.GetExecutionMode()
    
    switch executionMode {
    case workflow.ExecutionModeStreaming:
        // 流处理模式：创建 RealtimeInstanceManager
        manager, managerErr = e.createRealtimeInstanceManager(ctx, instance, wf)
        
    case workflow.ExecutionModeBatch:
        fallthrough  // 默认使用批处理模式
    default:
        // 批处理模式：创建 WorkflowInstanceManagerV2
        // 包括默认值（空值）和 "batch" 的情况
        manager, managerErr = e.createBatchInstanceManager(instance, wf)
    }
    
    if managerErr != nil {
        return nil, fmt.Errorf("创建WorkflowInstanceManager失败: %w", managerErr)
    }
    
    log.Printf("✅ 根据执行模式 %s 创建了对应的 InstanceManager", executionMode)

    // 创建WorkflowController
    controller := workflow.NewWorkflowControllerWithCallbacks(
        instance.ID,
        func() error {
            return e.PauseWorkflowInstance(ctx, instance.ID)
        },
        func() error {
            return e.ResumeWorkflowInstance(ctx, instance.ID)
        },
        func() error {
            return e.TerminateWorkflowInstance(ctx, instance.ID, "用户终止")
        },
        func() (string, error) {
            return e.GetWorkflowInstanceStatus(ctx, instance.ID)
        },
    )

    // 保存到内存映射
    e.mu.Lock()
    e.managers[instance.ID] = manager
    e.controllers[instance.ID] = controller
    e.mu.Unlock()

    // 启动状态更新转发协程：将Manager的状态更新转发到Controller
    go e.forwardStatusUpdates(instance.ID, manager, controller)

    // 立即启动WorkflowInstance执行
    manager.Start()

    // 如果Workflow启用了定时调度，注册到CronScheduler
    if wf.IsCronEnabled() && e.cronScheduler != nil {
        if err := e.cronScheduler.RegisterWorkflow(wf); err != nil {
            log.Printf("⚠️ 注册Workflow到定时调度器失败: WorkflowID=%s, Error=%v", wf.GetID(), err)
            // 不阻止Workflow提交，仅记录日志
        }
    }

    log.Printf("✅ 提交Workflow成功，创建WorkflowInstance: %s，执行模式: %s，已启动执行", 
        instance.ID, executionMode)
    return controller, nil
}

// createBatchInstanceManager 创建批处理 InstanceManager
func (e *Engine) createBatchInstanceManager(
    instance *workflow.WorkflowInstance,
    wf *workflow.Workflow,
) (types.WorkflowInstanceManager, error) {
    if e.instanceManagerVersion == InstanceManagerV2 {
        return NewWorkflowInstanceManagerV2WithAggregate(
            instance,
            wf,
            e.executor,
            e.aggregateRepo,
            e.taskRepo,
            e.workflowInstanceRepo,
            e.registry,
            e.pluginManager,
        )
    } else {
        return NewWorkflowInstanceManager(
            instance,
            wf,
            e.executor,
            e.taskRepo,
            e.workflowInstanceRepo,
            e.registry,
        )
    }
}

// createRealtimeInstanceManager 创建实时 InstanceManager
func (e *Engine) createRealtimeInstanceManager(
    ctx context.Context,
    instance *workflow.WorkflowInstance,
    wf *workflow.Workflow,
) (types.WorkflowInstanceManager, error) {
    // 检查是否为 RealtimeWorkflow
    rtWf, ok := wf.(*realtime.RealtimeWorkflow)
    if !ok {
        // 如果不是 RealtimeWorkflow，尝试转换
        // 这里需要根据实际设计决定如何处理
        return nil, fmt.Errorf("Workflow 不是 RealtimeWorkflow 类型，无法创建实时 InstanceManager")
    }
    
    // 创建 RealtimeInstanceManager
    return realtime.NewRealtimeInstanceManager(
        instance,
        rtWf,
        e.workflowInstanceRepo,
        // 选项配置
        realtime.WithBufferSize(10000),
        realtime.WithBackpressureThreshold(0.8),
    )
}
```

#### 3.3.2 恢复未完成实例

```go
// restoreUnfinishedInstances 恢复未完成的WorkflowInstance（更新版）
func (e *Engine) restoreUnfinishedInstances() error {
    // ... 现有代码 ...
    
    for _, instance := range instances {
        // 加载 Workflow 定义
        wf, err := e.loadWorkflow(ctx, instance.WorkflowID)
        if err != nil {
            log.Printf("⚠️ 加载Workflow失败: WorkflowID=%s, Error=%v", instance.WorkflowID, err)
            continue
        }
        
        // 校验执行模式（如果从数据库加载，可能值不合法）
        if err := validateExecutionMode(wf.ExecutionMode); err != nil {
            log.Printf("⚠️ Workflow执行模式无效: WorkflowID=%s, Mode=%s, Error=%v", 
                instance.WorkflowID, wf.ExecutionMode, err)
            // 使用默认值
            wf.ExecutionMode = workflow.ExecutionModeBatch
        }
        
        // 根据执行模式选择 Manager
        var manager types.WorkflowInstanceManager
        var managerErr error
        
        executionMode := wf.GetExecutionMode()
        
        switch executionMode {
        case workflow.ExecutionModeStreaming:
            manager, managerErr = e.createRealtimeInstanceManager(ctx, instance, wf)
        case workflow.ExecutionModeBatch:
            fallthrough
        default:
            manager, managerErr = e.createBatchInstanceManager(instance, wf)
        }
        
        if managerErr != nil {
            log.Printf("⚠️ 创建Manager失败: InstanceID=%s, ExecutionMode=%s, Error=%v", 
                instance.ID, executionMode, managerErr)
            continue
        }
        
        // 如果有断点数据，恢复状态
        if instance.Breakpoint != nil {
            if err := manager.RestoreFromBreakpoint(instance.Breakpoint); err != nil {
                log.Printf("⚠️ 恢复断点失败: InstanceID=%s, Error=%v", instance.ID, err)
            }
        }
        
        // ... 其余代码 ...
    }
}
```

### 3.3 扩展接口访问

如果需要访问实时特定功能，可以通过类型断言：

```go
// GetRealtimeInstanceManager 获取实时 InstanceManager（如果可用）
func (e *Engine) GetRealtimeInstanceManager(instanceID string) (realtime.RealtimeInstanceManager, error) {
    e.mu.RLock()
    manager, exists := e.managers[instanceID]
    e.mu.RUnlock()
    
    if !exists {
        return nil, fmt.Errorf("Instance %s 不存在", instanceID)
    }
    
    rtManager, ok := manager.(realtime.RealtimeInstanceManager)
    if !ok {
        return nil, fmt.Errorf("Instance %s 不是实时实例", instanceID)
    }
    
    return rtManager, nil
}

// 使用示例
rtManager, err := engine.GetRealtimeInstanceManager(instanceID)
if err == nil {
    metrics := rtManager.GetMetrics()
    log.Printf("实时指标: %+v", metrics)
}
```

---

## 四、Builder 扩展

### 4.1 WorkflowBuilder 扩展

```go
// pkg/core/builder/workflow_builder.go

// WithExecutionMode 设置执行模式（通用方法）
func (b *WorkflowBuilder) WithExecutionMode(mode workflow.ExecutionMode) *WorkflowBuilder {
    if err := b.workflow.SetExecutionMode(mode); err != nil {
        // Builder 中通常不返回错误，可以记录日志或panic
        log.Printf("警告: 设置执行模式失败: %v", err)
        // 或者使用默认值
        b.workflow.SetExecutionMode(workflow.ExecutionModeBatch)
    }
    return b
}

// WithStreamingMode 设置为流处理模式（便捷方法）
func (b *WorkflowBuilder) WithStreamingMode() *WorkflowBuilder {
    return b.WithExecutionMode(workflow.ExecutionModeStreaming)
}

// WithBatchMode 设置为批处理模式（便捷方法，显式设置）
func (b *WorkflowBuilder) WithBatchMode() *WorkflowBuilder {
    return b.WithExecutionMode(workflow.ExecutionModeBatch)
}
```

#### 4.1.1 Builder 中的校验

```go
// Build 构建 Workflow（更新版）
func (b *WorkflowBuilder) Build() (*workflow.Workflow, error) {
    // ... 现有构建逻辑 ...
    
    // 校验执行模式
    if err := validateExecutionMode(b.workflow.ExecutionMode); err != nil {
        return nil, fmt.Errorf("执行模式校验失败: %w", err)
    }
    
    // 如果执行模式为空，设置默认值
    if b.workflow.ExecutionMode == "" {
        b.workflow.ExecutionMode = workflow.ExecutionModeBatch
    }
    
    // 执行 Workflow 的完整校验（包含执行模式校验）
    if err := b.workflow.Validate(); err != nil {
        return nil, fmt.Errorf("Workflow校验失败: %w", err)
    }
    
    return b.workflow, nil
}
```

### 4.2 使用示例

```go
// 创建批处理 Workflow
batchWf, err := builder.NewWorkflowBuilder("batch_workflow", "批处理工作流").
    WithBatchMode().  // 显式设置（可选，默认就是批处理）
    WithTask(task1).
    Build()

// 创建实时 Workflow
realtimeWf, err := builder.NewRealtimeWorkflowBuilder("realtime_workflow", "实时工作流").
    WithStreamingMode().  // 设置为流处理模式
    WithRealtimeTask(rtTask).
    Build()
```

---

## 五、数据持久化

### 5.1 数据库字段

在数据库表中添加 `execution_mode` 字段：

```sql
-- Workflow 定义表
ALTER TABLE workflow_definition 
ADD COLUMN execution_mode VARCHAR(20) DEFAULT 'batch' 
COMMENT '执行模式: batch(批处理) 或 streaming(流处理)';

-- 添加索引（如果需要按执行模式查询）
CREATE INDEX idx_execution_mode ON workflow_definition(execution_mode);
```

### 5.2 Repository 支持

```go
// pkg/storage/workflow_repo.go

// SaveWorkflow 保存 Workflow（更新版）
func (r *workflowRepo) SaveWorkflow(ctx context.Context, wf *workflow.Workflow) error {
    // ... 现有保存逻辑 ...
    
    // 保存 execution_mode 字段
    query := `
        INSERT INTO workflow_definition (id, name, description, execution_mode, ...)
        VALUES (?, ?, ?, ?, ...)
        ON DUPLICATE KEY UPDATE 
            name = VALUES(name),
            description = VALUES(description),
            execution_mode = VALUES(execution_mode),
            ...
    `
    
    _, err := r.db.ExecContext(ctx, query,
        wf.ID,
        wf.Name,
        wf.Description,
        string(wf.ExecutionMode),  // 保存执行模式
        // ... 其他字段 ...
    )
    
    return err
}

// LoadWorkflow 加载 Workflow（更新版）
func (r *workflowRepo) LoadWorkflow(ctx context.Context, workflowID string) (*workflow.Workflow, error) {
    // ... 现有加载逻辑 ...
    
    var executionModeStr string
    err := r.db.QueryRowContext(ctx, 
        "SELECT id, name, description, execution_mode, ... FROM workflow_definition WHERE id = ?",
        workflowID,
    ).Scan(&wf.ID, &wf.Name, &wf.Description, &executionModeStr, ...)
    
    if err != nil {
        return nil, err
    }
    
    // 转换执行模式字符串为 ExecutionMode 类型
    wf.ExecutionMode = workflow.ExecutionMode(executionModeStr)
    
    // 校验执行模式（如果数据库中的值不合法，使用默认值）
    if err := validateExecutionMode(wf.ExecutionMode); err != nil {
        log.Printf("警告: Workflow %s 的执行模式无效: %s，使用默认值 batch", 
            workflowID, executionModeStr)
        wf.ExecutionMode = workflow.ExecutionModeBatch
    }
    
    return wf, nil
}
```

## 六、配置支持

### 5.1 Engine 配置扩展

```yaml
# configs/engine.yaml
engine:
  # InstanceManager 配置
  instance_manager:
    # 批处理模式默认版本
    batch_default_version: "v2"  # v1 或 v2
    
    # 实时模式配置
    realtime:
      buffer_size: 10000
      backpressure_threshold: 0.8
      reconnect_enabled: true
      max_reconnect_attempts: 0  # 0=无限重连
```

### 5.2 EngineBuilder 扩展

```go
// pkg/core/engine/builder.go

// WithRealtimeConfig 配置实时模式参数
func (b *EngineBuilder) WithRealtimeConfig(config *RealtimeConfig) *EngineBuilder {
    b.realtimeConfig = config
    return b
}

// RealtimeConfig 实时模式配置
type RealtimeConfig struct {
    BufferSize            int
    BackpressureThreshold float64
    ReconnectEnabled      bool
    MaxReconnectAttempts  int
}
```

---

## 七、向后兼容性

### 6.1 兼容性保证

| 场景 | 兼容性 | 说明 |
|------|--------|------|
| **现有批处理 Workflow** | ✅ 完全兼容 | 默认使用批处理模式，行为不变 |
| **现有 API** | ✅ 完全兼容 | `SubmitWorkflow` 接口不变 |
| **现有 Builder** | ✅ 完全兼容 | 不设置 ExecutionMode 时默认为批处理 |
| **现有配置** | ✅ 完全兼容 | 不配置实时参数时使用默认值 |

### 6.2 迁移路径

1. **无需迁移**：现有批处理 Workflow 无需修改
2. **可选升级**：可以显式设置 `WithBatchMode()` 明确意图
3. **新功能使用**：创建实时 Workflow 时使用 `WithStreamingMode()`

---

## 八、测试策略

### 7.1 单元测试

```go
func TestEngine_SubmitBatchWorkflow(t *testing.T) {
    // 测试批处理 Workflow 使用 WorkflowInstanceManagerV2
    engine := setupEngine()
    
    wf := builder.NewWorkflowBuilder("test", "test").
        WithBatchMode().
        WithTask(task1).
        Build()
    
    controller, err := engine.SubmitWorkflow(ctx, wf)
    assert.NoError(t, err)
    
    // 验证使用的是批处理 Manager
    manager := engine.GetInstanceManager(controller.InstanceID())
    _, isV2 := manager.(*WorkflowInstanceManagerV2)
    assert.True(t, isV2)
}

func TestEngine_SubmitRealtimeWorkflow(t *testing.T) {
    // 测试实时 Workflow 使用 RealtimeInstanceManager
    engine := setupEngine()
    
    rtWf := builder.NewRealtimeWorkflowBuilder("test", "test").
        WithStreamingMode().
        WithRealtimeTask(rtTask).
        Build()
    
    controller, err := engine.SubmitWorkflow(ctx, rtWf)
    assert.NoError(t, err)
    
    // 验证使用的是实时 Manager
    rtManager, err := engine.GetRealtimeInstanceManager(controller.InstanceID())
    assert.NoError(t, err)
    assert.NotNil(t, rtManager)
}
```

### 7.2 集成测试

```go
func TestEngine_MixedWorkflows(t *testing.T) {
    // 测试 Engine 同时支持批处理和实时 Workflow
    engine := setupEngine()
    
    // 提交批处理 Workflow
    batchWf := createBatchWorkflow()
    batchController, err := engine.SubmitWorkflow(ctx, batchWf)
    assert.NoError(t, err)
    
    // 提交实时 Workflow
    realtimeWf := createRealtimeWorkflow()
    realtimeController, err := engine.SubmitWorkflow(ctx, realtimeWf)
    assert.NoError(t, err)
    
    // 验证两个实例都正常运行
    assert.Equal(t, "Running", batchController.GetStatus())
    assert.Equal(t, "Running", realtimeController.GetStatus())
}
```

---

## 九、实施计划

### 9.1 阶段一：Workflow 字段扩展（2-3天）

1. ✅ 在 `Workflow` 结构体中添加 `ExecutionMode` 字段
2. ✅ 定义 `ExecutionMode` 类型和常量
3. ✅ 实现 `GetExecutionMode()` 方法（线程安全）
4. ✅ 实现 `SetExecutionMode()` 方法（带校验）
5. ✅ 实现 `validateExecutionMode()` 校验函数
6. ✅ 更新 `NewWorkflow()` 初始化方法
7. ✅ 更新 `Validate()` 方法，添加执行模式校验
8. ✅ 单元测试

### 9.2 阶段二：Engine 集成（2-3天）

1. ✅ 修改 `Engine.SubmitWorkflow()` 方法，添加执行模式检测
2. ✅ 实现 `createBatchInstanceManager()` 方法
3. ✅ 实现 `createRealtimeInstanceManager()` 方法
4. ✅ 实现 `validateStreamingWorkflow()` 校验方法
5. ✅ 更新 `restoreUnfinishedInstances()` 方法
6. ✅ 集成测试

### 8.2 阶段二：Builder 扩展（3天）

1. ✅ 扩展 `WorkflowBuilder` 添加 `WithStreamingMode()` 方法
2. ✅ 扩展 `RealtimeWorkflowBuilder` 支持执行模式设置
3. ✅ 更新使用示例

### 8.3 阶段三：测试和文档（3天）

1. ✅ 编写单元测试
2. ✅ 编写集成测试
3. ✅ 更新文档

---

## 十、校验逻辑总结

### 10.1 校验层次

| 层次 | 校验位置 | 校验内容 | 失败处理 |
|------|---------|---------|---------|
| **字段级** | `SetExecutionMode()` | 执行模式值是否合法（batch/streaming） | 返回错误，不设置值 |
| **对象级** | `Workflow.Validate()` | 执行模式值合法性 | 返回错误，阻止提交 |
| **Engine级** | `Engine.SubmitWorkflow()` | 执行模式与任务类型兼容性 | 返回错误，阻止提交 |
| **恢复级** | `restoreUnfinishedInstances()` | 从数据库加载的执行模式合法性 | 使用默认值，记录警告 |

### 10.2 校验规则

#### 规则 1：执行模式值校验

```go
// 允许的值
- "batch"     → 批处理模式
- "streaming" → 流处理模式
- ""          → 空值，使用默认值 "batch"

// 不允许的值
- 任何其他字符串 → 返回错误
```

#### 规则 2：默认值处理

```go
// 场景 1: 新建 Workflow
wf := workflow.NewWorkflow("test", "test")
// ExecutionMode 自动设置为 "batch"

// 场景 2: 从数据库加载（字段为空）
wf.ExecutionMode = ""  // 数据库中没有该字段（旧数据）
wf.GetExecutionMode()  // 返回 "batch"（默认值）

// 场景 3: 从数据库加载（字段值无效）
wf.ExecutionMode = "invalid"
// 在 restoreUnfinishedInstances 中检测到无效值，重置为 "batch"
```

#### 规则 3：流处理模式兼容性校验

```go
// 流处理模式需要检查：
// 1. 是否有实时任务（如果有 RealtimeWorkflow 类型）
// 2. 任务配置是否支持流处理
// 3. 其他业务规则（根据实际需求）
```

### 10.3 校验流程图

```
┌─────────────────────────────────────────────────────────┐
│              Workflow 执行模式校验流程                    │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
        ┌───────────────────────────────────┐
        │  SetExecutionMode(mode)           │
        │  - 校验 mode 值是否合法            │
        │  - 合法：设置值并返回 nil          │
        │  - 不合法：返回错误，不设置值       │
        └───────────────┬───────────────────┘
                        │
                        ▼
        ┌───────────────────────────────────┐
        │  Workflow.Validate()               │
        │  - 调用 validateExecutionMode()    │
        │  - 检查执行模式与任务兼容性         │
        │  - 失败：返回错误                  │
        └───────────────┬───────────────────┘
                        │
                        ▼
        ┌───────────────────────────────────┐
        │  Engine.SubmitWorkflow()            │
        │  - 调用 wf.Validate()              │
        │  - 流处理模式：额外校验              │
        │  - 失败：返回错误，阻止提交          │
        └───────────────┬───────────────────┘
                        │
                        ▼
        ┌───────────────────────────────────┐
        │  根据执行模式选择 Manager           │
        │  - batch → WorkflowInstanceManagerV2│
        │  - streaming → RealtimeInstanceManager│
        └───────────────────────────────────┘
```

## 十一、总结

### 11.1 设计要点

1. **字段设计**：在 `Workflow` 中添加 `ExecutionMode` 字段，类型为 `ExecutionMode`（string 别名）
2. **默认值**：默认值为 `"batch"`，保证向后兼容
3. **校验机制**：多层次校验（字段级、对象级、Engine级、恢复级）
4. **策略选择**：Engine 根据 `ExecutionMode` 字段自动选择对应的 Manager
5. **接口统一**：所有 Manager 都实现 `types.WorkflowInstanceManager` 接口

### 11.2 关键优势

- ✅ **简单直接**：通过字段值判断，无需类型断言
- ✅ **自动选择**：Engine 自动根据 Workflow 类型选择 Manager
- ✅ **接口统一**：Engine 通过统一接口使用，无需关心具体实现
- ✅ **向后兼容**：现有批处理 Workflow 无需修改
- ✅ **数据持久化**：执行模式可以保存到数据库
- ✅ **类型安全**：通过类型断言访问实时特定功能
- ✅ **易于扩展**：未来可以支持更多类型的 Manager
- ✅ **校验完善**：多层次校验确保数据合法性

---

*文档版本: V1.0*  
*创建时间: 2026-01-07*

