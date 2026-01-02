package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
	"github.com/stevelan1995/task-engine/pkg/storage/dao"
)

// workflowRepo SQLite实现（小写，不导出）
type workflowRepo struct {
	db *sqlx.DB
}

// NewWorkflowRepo 创建SQLite存储实例（内部工厂方法，不导出）
// dsn: 数据库连接字符串（如"./data.db"）
func NewWorkflowRepo(dsn string) (storage.WorkflowRepository, error) {
	db, err := sqlx.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 配置SQLite：启用WAL模式和其他优化设置
	if err := configureSQLite(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("配置SQLite失败: %w", err)
	}

	repo := &workflowRepo{
		db: db,
	}

	// 初始化表结构
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}

	return repo, nil
}

// initSchema 初始化数据库表结构（内部方法）
func (r *workflowRepo) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS workflow_definition (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		params TEXT,
		dependencies TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'ENABLED',
		sub_task_error_tolerance REAL NOT NULL DEFAULT 0.0,
		transactional INTEGER NOT NULL DEFAULT 0,
		transaction_mode TEXT DEFAULT '',
		max_concurrent_task INTEGER NOT NULL DEFAULT 10
	);
	`
	_, err := r.db.Exec(createTableSQL)
	if err != nil {
		return err
	}

	// 如果表已存在，添加新字段（ALTER TABLE 在 SQLite 中需要特殊处理）
	// 检查字段是否存在，如果不存在则添加
	alterSQLs := []string{
		`ALTER TABLE workflow_definition ADD COLUMN sub_task_error_tolerance REAL NOT NULL DEFAULT 0.0`,
		`ALTER TABLE workflow_definition ADD COLUMN transactional INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE workflow_definition ADD COLUMN transaction_mode TEXT DEFAULT ''`,
		`ALTER TABLE workflow_definition ADD COLUMN max_concurrent_task INTEGER NOT NULL DEFAULT 10`,
	}

	for _, alterSQL := range alterSQLs {
		// SQLite 的 ALTER TABLE ADD COLUMN 如果列已存在会报错，忽略错误
		_, _ = r.db.Exec(alterSQL)
	}

	return nil
}

// Save 实现存储接口（内部实现）
func (r *workflowRepo) Save(ctx context.Context, wf *workflow.Workflow) error {
	// 序列化依赖关系
	depsJSON, err := json.Marshal(wf.GetDependencies())
	if err != nil {
		return fmt.Errorf("序列化依赖关系失败: %w", err)
	}

	// 序列化参数（sync.Map需要先转换为普通map）
	paramsMap := make(map[string]string)
	wf.Params.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(string); ok {
				paramsMap[k] = v
			}
		}
		return true
	})
	paramsJSON, err := json.Marshal(paramsMap)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	// 构建DAO对象
	dao := &dao.WorkflowDAO{
		ID:                    wf.GetID(),
		Name:                  wf.GetName(),
		Description:           wf.Description,
		Params:                string(paramsJSON),
		Dependencies:          string(depsJSON),
		CreateTime:            wf.CreateTime,
		Status:                wf.GetStatus(),
		SubTaskErrorTolerance: wf.GetSubTaskErrorTolerance(),
		Transactional:         wf.GetTransactional(),
		TransactionMode:       wf.GetTransactionMode(),
		MaxConcurrentTask:      wf.GetMaxConcurrentTask(),
	}

	query := `
	INSERT OR REPLACE INTO workflow_definition (id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task)
	VALUES (:id, :name, :description, :params, :dependencies, :create_time, :status, :sub_task_error_tolerance, :transactional, :transaction_mode, :max_concurrent_task)
	`
	_, err = r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存Workflow失败: %w", err)
	}
	return nil
}

// SaveWithTasks 在事务中同时保存Workflow和所有预定义的Task（内部实现）
func (r *workflowRepo) SaveWithTasks(ctx context.Context, wf *workflow.Workflow, workflowInstanceID string, taskRepo storage.TaskRepository, registry interface{}) error {
	// 开始事务
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 保存Workflow
	depsJSON, err := json.Marshal(wf.GetDependencies())
	if err != nil {
		return fmt.Errorf("序列化依赖关系失败: %w", err)
	}

	// 序列化参数（sync.Map需要先转换为普通map）
	paramsMap := make(map[string]string)
	wf.Params.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(string); ok {
				paramsMap[k] = v
			}
		}
		return true
	})
	paramsJSON, err := json.Marshal(paramsMap)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	workflowDAO := &dao.WorkflowDAO{
		ID:                    wf.GetID(),
		Name:                  wf.GetName(),
		Description:           wf.Description,
		Params:                string(paramsJSON),
		Dependencies:          string(depsJSON),
		CreateTime:            wf.CreateTime,
		Status:                wf.GetStatus(),
		SubTaskErrorTolerance: wf.GetSubTaskErrorTolerance(),
		Transactional:         wf.GetTransactional(),
		TransactionMode:       wf.GetTransactionMode(),
		MaxConcurrentTask:      wf.GetMaxConcurrentTask(),
	}

	query := `
	INSERT OR REPLACE INTO workflow_definition (id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task)
	VALUES (:id, :name, :description, :params, :dependencies, :create_time, :status, :sub_task_error_tolerance, :transactional, :transaction_mode, :max_concurrent_task)
	`
	_, err = tx.NamedExecContext(ctx, query, workflowDAO)
	if err != nil {
		return fmt.Errorf("保存Workflow失败: %w", err)
	}

	// 2. 保存所有预定义的Task
	tasks := wf.GetTasks()
	if len(tasks) > 0 {
		// 获取registry以获取JobFuncID
		var getJobFuncID func(string) string
		if registry != nil {
			// 尝试类型断言为 *task.FunctionRegistry
			if reg, ok := registry.(interface {
				GetIDByName(name string) string
			}); ok {
				getJobFuncID = reg.GetIDByName
			}
		}

		for taskID, t := range tasks {
			jobFuncID := ""
			if getJobFuncID != nil {
				jobFuncID = getJobFuncID(t.GetJobFuncName())
			}

			// 序列化参数
			paramsJSON, err := json.Marshal(t.GetParams())
			if err != nil {
				return fmt.Errorf("序列化Task %s 参数失败: %w", taskID, err)
			}

			// 获取超时时间（从Task配置中获取，如果没有则使用默认值30）
			timeoutSeconds := 30
			if taskObj, ok := t.(*task.Task); ok {
				if taskObj.TimeoutSeconds > 0 {
					timeoutSeconds = taskObj.TimeoutSeconds
				}
			}

			// 构建TaskDAO对象
			taskDAO := &dao.TaskDAO{
				ID:                 taskID,
				Name:               t.GetName(),
				WorkflowInstanceID: workflowInstanceID,
				JobFuncName:        t.GetJobFuncName(),
				Params:             string(paramsJSON),
				Status:             "Pending",
				TimeoutSeconds:     timeoutSeconds,
				RetryCount:         0,
				CreateTime:         time.Now(),
			}

			if jobFuncID != "" {
				taskDAO.JobFuncID.Valid = true
				taskDAO.JobFuncID.String = jobFuncID
			}

			// 保存Task到数据库（在事务中）
			taskQuery := `
			INSERT OR REPLACE INTO task_instance 
			(id, name, workflow_instance_id, job_func_id, job_func_name, params, status, 
			 timeout_seconds, retry_count, start_time, end_time, error_msg, create_time)
			VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :params, :status, 
			 :timeout_seconds, :retry_count, :start_time, :end_time, :error_msg, :create_time)
			`
			_, err = tx.NamedExecContext(ctx, taskQuery, taskDAO)
			if err != nil {
				return fmt.Errorf("保存Task %s (%s) 失败: %w", taskID, t.GetName(), err)
			}
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetByID 实现存储接口（内部实现）
func (r *workflowRepo) GetByID(ctx context.Context, id string) (*workflow.Workflow, error) {
	var dao dao.WorkflowDAO
	query := `SELECT id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task FROM workflow_definition WHERE id = ?`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询Workflow失败: %w", err)
	}

	// 反序列化依赖关系
	var deps map[string][]string
	if dao.Dependencies != "" {
		if err := json.Unmarshal([]byte(dao.Dependencies), &deps); err != nil {
			return nil, fmt.Errorf("反序列化依赖关系失败: %w", err)
		}
	} else {
		deps = make(map[string][]string)
	}

	// 反序列化参数
	var params map[string]string
	if dao.Params != "" {
		if err := json.Unmarshal([]byte(dao.Params), &params); err != nil {
			return nil, fmt.Errorf("反序列化参数失败: %w", err)
		}
	} else {
		params = make(map[string]string)
	}

	wf := workflow.NewWorkflow(dao.Name, dao.Description)
	wf.ID = dao.ID
	// 将map转换为sync.Map
	for k, v := range deps {
		wf.Dependencies.Store(k, v)
	}
	// 将map转换为sync.Map
	for k, v := range params {
		wf.Params.Store(k, v)
	}
	wf.CreateTime = dao.CreateTime
	wf.SetStatus(dao.Status)
	// 设置新字段
	if err := wf.SetSubTaskErrorTolerance(dao.SubTaskErrorTolerance); err != nil {
		return nil, fmt.Errorf("设置子任务错误容忍度失败: %w", err)
	}
	if err := wf.SetTransactional(dao.Transactional); err != nil {
		return nil, fmt.Errorf("设置事务模式失败: %w", err)
	}
	wf.SetTransactionMode(dao.TransactionMode)
	if err := wf.SetMaxConcurrentTask(dao.MaxConcurrentTask); err != nil {
		return nil, fmt.Errorf("设置最大并发任务数失败: %w", err)
	}
	// 注意：Tasks需要从其他地方加载，这里只加载定义

	return wf, nil
}

// Delete 实现存储接口（内部实现）
// 幂等性：删除不存在的记录不会报错
func (r *workflowRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM workflow_definition WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("删除Workflow失败: %w", err)
	}
	// 不检查 RowsAffected，删除不存在的记录也是幂等的
	return nil
}
