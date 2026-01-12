### 一、核心设计思路（API 语义优化+链式Builder+非阻塞架构）

#### 1. API 名称优化原则

- **语义化**：贴合“工作流引擎”的行业通用术语，避免歧义；
- **简洁性**：去掉冗余词汇，保留核心语义；
- **一致性**：动词/名词格式统一（如 `WithXXX`/`LoadXXX`/`SubmitXXX`）。

#### 2. 核心API 命名最终方案

| 原API 意图         | 优化后API 名称                  | 语义说明                                          |
| ------------------ | ------------------------------- | ------------------------------------------------- |
| 引擎构建器初始化   | `NewEngineBuilder`            | 基础构建器，传入引擎配置文件                      |
| 注册Job 函数       | `WithJobFunc`                 | 保留（语义清晰，Job 贴合任务语义）                |
| 注册Callback 函数  | `WithCallbackFunc`            | 保留（Callback 是行业通用术语）                   |
| 注册依赖           | `WithService`                 | 替代 `WithDependency`（更贴合“服务依赖”语义） |
| 构建引擎           | `Build()`                     | 保留（Builder 模式标准）                          |
| 启动引擎（初始化） | `Start()`                     | 保留（非阻塞，仅初始化资源）                      |
| 加载工作流定义     | `LoadWorkflow`                | 替代 `LoadWorkFlowDefinition`（简洁，去掉冗余） |
| 提交工作流运行     | `SubmitWorkflow`              | 保留（语义清晰）                                  |
| 工作流控制器       | `WorkflowController`          | 保留（管控工作流生命周期）                        |
| 等待工作流完成     | `WorkflowController.Wait()`   | 替代 `WaitUntilComplete`（简洁）                |
| 查询工作流状态     | `WorkflowController.Status()` | 补充（非阻塞状态查询）                            |

### 二、完整代码实现（核心结构体+API）

#### 1. 引擎核心定义（`engine/engine.go`）

```go
package engine

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"your-module-path/config"
	"your-module-path/workflow"
)

// ========== 核心类型定义 ==========
// Engine 工作流引擎（替代原TaskFlow）
type Engine struct {
	cfg             *config.EngineConfig       // 引擎配置
	jobRegistry     map[string]JobFunc         // Job函数注册表
	callbackRegistry map[string]CallbackFunc   // Callback函数注册表
	serviceRegistry map[string]interface{}     // 服务依赖注册表
	workflowManager *workflow.Manager          // 工作流管理器
	isStarted       bool                       // 引擎是否已启动
	mu              sync.RWMutex               // 并发安全锁
}

// JobFunc 任务函数签名（原TaskFlow的JobFunc）
type JobFunc func(ctx *workflow.Context) (interface{}, error)

// CallbackFunc 回调函数签名（原TaskFlow的CallbackFunc）
type CallbackFunc func(ctx *workflow.Context) error

// WorkflowDefinition 工作流定义（封装配置文件加载结果）
type WorkflowDefinition struct {
	ID          string                 // 工作流ID
	Config      *workflow.Config       // 工作流配置
	SourcePath  string                 // 配置文件路径
}

// WorkflowController 工作流控制器（提交后返回，管控生命周期）
type WorkflowController struct {
	instanceID  string                 // 工作流实例ID
	status      workflow.Status        // 当前状态
	doneChan    chan struct{}          // 完成信号通道
	engine      *Engine                // 关联引擎
}

// ========== Builder 模式定义 ==========
// EngineBuilder 引擎构建器（链式调用）
type EngineBuilder struct {
	engineConfigPath string               // 引擎配置文件路径
	jobFuncs         map[string]JobFunc   // 待注册的Job函数
	callbackFuncs    map[string]CallbackFunc // 待注册的Callback函数
	services         map[string]interface{}  // 待注册的服务依赖
	err              error                // 构建过程中的错误
}

// ========== Builder 核心方法 ==========
// NewEngineBuilder 创建引擎构建器（入口）
func NewEngineBuilder(engineConfigPath string) *EngineBuilder {
	return &EngineBuilder{
		engineConfigPath: engineConfigPath,
		jobFuncs:         make(map[string]JobFunc),
		callbackFuncs:    make(map[string]CallbackFunc),
		services:         make(map[string]interface{}),
	}
}

// WithJobFunc 注册Job函数（链式）
func (b *EngineBuilder) WithJobFunc(funcKey string, fn JobFunc) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if funcKey == "" || fn == nil {
		b.err = errors.New("job func key or function is empty")
		return b
	}
	b.jobFuncs[funcKey] = fn
	return b
}

// WithCallbackFunc 注册Callback函数（链式）
func (b *EngineBuilder) WithCallbackFunc(funcKey string, fn CallbackFunc) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if funcKey == "" || fn == nil {
		b.err = errors.New("callback func key or function is empty")
		return b
	}
	b.callbackFuncs[funcKey] = fn
	return b
}

// WithService 注册服务依赖（替代WithDependency，语义更优）
func (b *EngineBuilder) WithService(serviceKey string, service interface{}) *EngineBuilder {
	if b.err != nil {
		return b
	}
	if serviceKey == "" || service == nil {
		b.err = errors.New("service key or instance is empty")
		return b
	}
	b.services[serviceKey] = service
	return b
}

// Build 构建引擎实例（最终步骤）
func (b *EngineBuilder) Build() (*Engine, error) {
	// 检查构建过程是否有错误
	if b.err != nil {
		return nil, b.err
	}

	// 1. 加载引擎配置
	cfg, err := config.LoadEngineConfig(b.engineConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load engine config failed: %w", err)
	}

	// 2. 初始化工作流管理器
	workflowManager, err := workflow.NewManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("init workflow manager failed: %w", err)
	}

	// 3. 构建引擎实例
	engine := &Engine{
		cfg:             cfg,
		jobRegistry:     b.jobFuncs,
		callbackRegistry: b.callbackFuncs,
		serviceRegistry: b.services,
		workflowManager: workflowManager,
		isStarted:       false,
	}

	return engine, nil
}

// ========== Engine 核心方法 ==========
// Start 启动引擎（非阻塞）：初始化DB、缓存、依赖容器等
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isStarted {
		return errors.New("engine already started")
	}

	// 1. 初始化底层资源（DB、缓存等）
	if err := e.initResources(); err != nil {
		return fmt.Errorf("init engine resources failed: %w", err)
	}

	// 2. 标记引擎已启动
	e.isStarted = true
	fmt.Println("engine started successfully")
	return nil
}

// LoadWorkflow 加载工作流定义（从文件）
func (e *Engine) LoadWorkflow(workflowConfigPath string) (*WorkflowDefinition, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.isStarted {
		return nil, errors.New("engine not started, call Start() first")
	}

	// 加载并解析工作流配置
	wfConfig, err := workflow.LoadConfig(workflowConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load workflow config failed: %w", err)
	}

	return &WorkflowDefinition{
		ID:          wfConfig.ID,
		Config:      wfConfig,
		SourcePath:  workflowConfigPath,
	}, nil
}

// SubmitWorkflow 提交工作流运行（非阻塞），返回控制器
func (e *Engine) SubmitWorkflow(wfDef *WorkflowDefinition) (*WorkflowController, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.isStarted {
		return nil, errors.New("engine not started, call Start() first")
	}
	if wfDef == nil || wfDef.Config == nil {
		return nil, errors.New("invalid workflow definition")
	}

	// 1. 创建工作流实例
	instanceID := fmt.Sprintf("wf-%s-%d", wfDef.ID, time.Now().UnixNano())
	doneChan := make(chan struct{})

	// 2. 异步运行工作流（非阻塞）
	go func() {
		defer close(doneChan)
		if err := e.workflowManager.Run(wfDef.Config, instanceID, e.jobRegistry, e.callbackRegistry, e.serviceRegistry); err != nil {
			fmt.Printf("workflow %s run failed: %v\n", instanceID, err)
		}
	}()

	// 3. 返回控制器
	return &WorkflowController{
		instanceID: instanceID,
		status:     workflow.StatusRunning,
		doneChan:   doneChan,
		engine:     e,
	}, nil
}

// ========== WorkflowController 核心方法 ==========
// Wait 阻塞等待工作流完成（用户按需调用）
func (c *WorkflowController) Wait() error {
	<-c.doneChan
	// 更新状态
	c.status = c.engine.workflowManager.GetStatus(c.instanceID)
	if c.status != workflow.StatusCompleted {
		return fmt.Errorf("workflow %s finished with status: %s", c.instanceID, c.status)
	}
	return nil
}

// Status 查询工作流当前状态（非阻塞）
func (c *WorkflowController) Status() workflow.Status {
	return c.engine.workflowManager.GetStatus(c.instanceID)
}

// Cancel 取消工作流运行（非阻塞）
func (c *WorkflowController) Cancel() error {
	return c.engine.workflowManager.Cancel(c.instanceID)
}

// ========== 私有辅助方法 ==========
// initResources 初始化引擎底层资源（DB、缓存等）
func (e *Engine) initResources() error {
	// 1. 初始化数据库连接（SQLite/PostgreSQL）
	if err := initDB(e.cfg.Storage); err != nil {
		return err
	}
	// 2. 初始化缓存
	if err := initCache(e.cfg.Cache); err != nil {
		return err
	}
	// 3. 其他资源初始化...
	return nil
}
```

#### 2. 配置定义（`config/engine.go`）

```go
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// EngineConfig 引擎配置（原框架配置）
type EngineConfig struct {
	General struct {
		InstanceName string `yaml:"instance_name"`
		LogLevel     string `yaml:"log_level"`
		Env          string `yaml:"env"`
	} `yaml:"general"`
	Storage struct {
		Database struct {
			Type             string        `yaml:"type"`
			DSN              string        `yaml:"dsn"`
			MaxOpenConns     int           `yaml:"max_open_conns"`
			MaxIdleConns     int           `yaml:"max_idle_conns"`
			ConnMaxLifetime  time.Duration `yaml:"conn_max_lifetime"`
			ConnMaxIdleTime  time.Duration `yaml:"conn_max_idle_time"`
		} `yaml:"database"`
		Cache struct {
			Enabled        bool          `yaml:"enabled"`
			DefaultTTL     time.Duration `yaml:"default_ttl"`
			CleanInterval  time.Duration `yaml:"clean_interval"`
		} `yaml:"cache"`
	} `yaml:"storage"`
	Execution struct {
		DefaultTaskTimeout time.Duration `yaml:"default_task_timeout"`
		WorkerConcurrency  int            `yaml:"worker_concurrency"`
		Retry struct {
			Enabled     bool          `yaml:"enabled"`
			MaxAttempts int           `yaml:"max_attempts"`
			Delay       time.Duration `yaml:"delay"`
			MaxDelay    time.Duration `yaml:"max_delay"`
		} `yaml:"retry"`
	} `yaml:"execution"`
}

// LoadEngineConfig 加载引擎配置
func LoadEngineConfig(path string) (*EngineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg EngineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
```

#### 3. 工作流状态定义（`workflow/status.go`）

```go
package workflow

// Status 工作流状态
type Status string

const (
	StatusPending   Status = "pending"    // 待运行
	StatusRunning   Status = "running"    // 运行中
	StatusCompleted Status = "completed"  // 完成
	StatusFailed    Status = "failed"     // 失败
	StatusCancelled Status = "cancelled"  // 已取消
)
```

### 三、用户侧使用示例（`main.go`）

```go
package main

import (
	"fmt"

	"your-module-path/engine"
	"your-module-path/tushare"
)

func main() {
	// ========== 1. 构建引擎（链式调用） ==========
	engineIns, err := engine.NewEngineBuilder("./config/engine/dev.yaml").
		// 注册Tushare同步相关Job函数
		WithJobFunc("tradeCalJob", tushare.TradeCalJob).
		WithJobFunc("dailyDownloadJob", tushare.DailyDownloadJob).
		WithJobFunc("financeDownloadJob", tushare.FinanceDownloadJob).
		// 注册回调函数
		WithCallbackFunc("syncSuccessCallback", tushare.SyncSuccessCallback).
		WithCallbackFunc("syncFailedCallback", tushare.SyncFailedCallback).
		// 注册业务依赖（服务/仓储）
		WithService("TushareClient", tushare.NewClient(tushare.TushareSyncConfig{
			Token:     "your-tushare-token",
			StartDate: "20251201",
			EndDate:   "20251225",
			Exchange:  "SSE",
		})).
		WithService("TushareRepository", tushare.NewRepository()).
		// 构建引擎实例
		Build()
	if err != nil {
		panic(fmt.Sprintf("build engine failed: %v", err))
	}

	// ========== 2. 启动引擎（非阻塞） ==========
	if err := engineIns.Start(); err != nil {
		panic(fmt.Sprintf("start engine failed: %v", err))
	}

	// ========== 3. 用户侧其他初始化操作（非阻塞，按需执行） ==========
	fmt.Println("do other init jobs... (e.g. init logger, register metrics)")
	// 示例：初始化日志、监控、其他服务等
	// initLogger()
	// initMetrics()

	// ========== 4. 加载工作流定义（从文件） ==========
	wfDef, err := engineIns.LoadWorkflow("./config/workflows/tushare-sync.yaml")
	if err != nil {
		panic(fmt.Sprintf("load workflow failed: %v", err))
	}

	// ========== 5. 提交工作流运行（非阻塞） ==========
	wfCtrl, err := engineIns.SubmitWorkflow(wfDef)
	if err != nil {
		panic(fmt.Sprintf("submit workflow failed: %v", err))
	}

	// ========== 6. 用户侧可选操作 ==========
	// 非阻塞查询状态
	fmt.Printf("workflow %s status: %s\n", wfCtrl.InstanceID(), wfCtrl.Status())

	// 按需阻塞等待完成（用户可选择是否调用）
	// if err := wfCtrl.Wait(); err != nil {
	// 	fmt.Printf("workflow run failed: %v\n", err)
	// } else {
	// 	fmt.Println("workflow run completed successfully")
	// }

	// 继续执行其他逻辑（非阻塞）
	fmt.Println("engine is running, workflow is processing...")
	select {} // 阻塞主线程（用户按需调整）
}
```

### 四、API 设计优化说明

#### 1. 核心命名优化理由

- **TaskFlow → Engine**：更贴合“工作流引擎”的通用语义，TaskFlow 偏底层，Engine 对用户更友好；
- **WithDependency → WithService**：Dependency 偏“依赖”的抽象概念，Service 更贴合业务层“服务实例”的语义，用户更容易理解；
- **LoadWorkFlowDefinition → LoadWorkflow**：去掉冗余的 Definition，简洁且语义不变；
- **WaitUntilComplete → Wait**：简洁，且行业内通用（如 channel 的 Wait、sync.WaitGroup 的 Wait）；
- **SubmitWorkflow**：保留，语义清晰（“提交工作流运行”）；
- **WorkflowController**：保留，贴合“管控工作流生命周期”的定位。

#### 2. 非阻塞设计核心

- `Engine.Start()`：仅初始化资源（DB、缓存等），不阻塞，用户可继续初始化其他组件；
- `Engine.SubmitWorkflow()`：异步启动工作流，返回控制器，不阻塞主线程；
- `WorkflowController.Wait()`：用户按需调用，不强制阻塞，满足“用户代码还要初始化其它东西”的需求。

#### 3. 扩展性设计

- Builder 模式支持后续扩展（如 `WithLogConfig`、`WithMetrics` 等），不破坏现有API；
- `WorkflowController` 提供 `Status()`/`Cancel()` 等非阻塞方法，满足用户对工作流的精细化管控；
- 引擎配置与工作流配置分离，加载/提交解耦，用户可加载多个工作流、多次提交运行。

### 五、补充说明

- 如果用户坚持用 `WithDependency`（而非 `WithService`），仅需将 `WithService` 重命名为 `WithDependency` 即可，逻辑完全兼容；
- `Engine.Start()` 可扩展为返回 `<-chan struct{}`，用户可监听引擎启动完成信号；
- `WorkflowController` 可补充 `InstanceID()` 方法，方便用户获取实例ID用于日志/监控。

该API设计兼顾了语义清晰、链式调用的便捷性、非阻塞的灵活性，同时符合行业通用的命名习惯，用户使用体验更优。
