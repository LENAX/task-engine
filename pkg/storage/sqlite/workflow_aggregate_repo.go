package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/storage"
	"github.com/LENAX/task-engine/pkg/storage/dao"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// WorkflowAggregateRepo Workflow聚合根Repository的SQLite实现（对外导出）
type WorkflowAggregateRepo struct {
	db *sqlx.DB
}

// NewWorkflowAggregateRepo 创建Workflow聚合根Repository实例（对外导出）
func NewWorkflowAggregateRepo(db *sqlx.DB) (*WorkflowAggregateRepo, error) {
	repo := &WorkflowAggregateRepo{db: db}
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	return repo, nil
}

// NewWorkflowAggregateRepoFromDSN 通过DSN创建Workflow聚合根Repository实例（对外导出）
func NewWorkflowAggregateRepoFromDSN(dsn string) (*WorkflowAggregateRepo, error) {
	db, err := sqlx.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 配置SQLite优化
	if err := configureSQLite(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("配置SQLite失败: %w", err)
	}

	return NewWorkflowAggregateRepo(db)
}

// configureSQLite 配置SQLite数据库连接
func configureSQLite(db *sqlx.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=30000;",
		"PRAGMA wal_autocheckpoint=1000;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	return nil
}

// GetDB 获取底层数据库连接（对外导出）
func (r *WorkflowAggregateRepo) GetDB() *sqlx.DB {
	return r.db
}

// Close 关闭数据库连接（对外导出）
func (r *WorkflowAggregateRepo) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// initSchema 初始化数据库表结构
func (r *WorkflowAggregateRepo) initSchema() error {
	// Workflow定义表
	createWorkflowSQL := `
	CREATE TABLE IF NOT EXISTS workflow_definition (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		description TEXT,
		params TEXT,
		dependencies TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'ENABLED',
		sub_task_error_tolerance REAL NOT NULL DEFAULT 0.0,
		transactional INTEGER NOT NULL DEFAULT 0,
		transaction_mode TEXT DEFAULT '',
		max_concurrent_task INTEGER NOT NULL DEFAULT 10,
		cron_expr TEXT DEFAULT '',
		cron_enabled INTEGER NOT NULL DEFAULT 0
	);
	`

	// Task定义表
	createTaskDefSQL := `
	CREATE TABLE IF NOT EXISTS task_definition (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		job_func_id TEXT,
		job_func_name TEXT,
		compensation_func_id TEXT,
		compensation_func_name TEXT,
		params TEXT,
		timeout_seconds INTEGER DEFAULT 30,
		retry_count INTEGER DEFAULT 0,
		dependencies TEXT,
		required_params TEXT,
		result_mapping TEXT,
		status_handlers TEXT,
		is_template INTEGER DEFAULT 0,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_id) REFERENCES workflow_definition(id) ON DELETE CASCADE,
		UNIQUE(workflow_id, name)
	);
	CREATE INDEX IF NOT EXISTS idx_task_definition_workflow_id ON task_definition(workflow_id);
	`

	// WorkflowInstance表
	createInstanceSQL := `
	CREATE TABLE IF NOT EXISTS workflow_instance (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		status TEXT NOT NULL,
		start_time DATETIME,
		end_time DATETIME,
		breakpoint TEXT,
		error_message TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_id) REFERENCES workflow_definition(id)
	);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_workflow_id ON workflow_instance(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_status ON workflow_instance(status);
	`

	// TaskInstance表
	createTaskInstSQL := `
	CREATE TABLE IF NOT EXISTS task_instance (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		workflow_instance_id TEXT NOT NULL,
		job_func_id TEXT,
		job_func_name TEXT,
		compensation_func_id TEXT,
		compensation_func_name TEXT,
		params TEXT,
		status TEXT NOT NULL,
		timeout_seconds INTEGER DEFAULT 30,
		retry_count INTEGER DEFAULT 0,
		start_time DATETIME,
		end_time DATETIME,
		error_msg TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_instance_id) REFERENCES workflow_instance(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_task_instance_workflow_instance_id ON task_instance(workflow_instance_id);
	CREATE INDEX IF NOT EXISTS idx_task_instance_status ON task_instance(status);
	`

	// Job函数元数据表
	createJobFunctionMetaSQL := `
	CREATE TABLE IF NOT EXISTS job_function_meta (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		description TEXT,
		code_path TEXT,
		hash TEXT,
		param_types TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_job_function_meta_name ON job_function_meta(name);
	`

	// Task Handler元数据表
	createTaskHandlerMetaSQL := `
	CREATE TABLE IF NOT EXISTS task_handler_meta (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		description TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_task_handler_meta_name ON task_handler_meta(name);
	`

	// 补偿函数元数据表
	createCompensationFunctionMetaSQL := `
	CREATE TABLE IF NOT EXISTS compensation_function_meta (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		description TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_compensation_function_meta_name ON compensation_function_meta(name);
	`

	for _, sql := range []string{
		createWorkflowSQL,
		createTaskDefSQL,
		createInstanceSQL,
		createTaskInstSQL,
		createJobFunctionMetaSQL,
		createTaskHandlerMetaSQL,
		createCompensationFunctionMetaSQL,
	} {
		if _, err := r.db.Exec(sql); err != nil {
			return fmt.Errorf("执行SQL失败: %w", err)
		}
	}

	return nil
}

// ========== Workflow定义相关操作 ==========

// SaveWorkflow 保存Workflow及其关联的Task定义（事务）
func (r *WorkflowAggregateRepo) SaveWorkflow(ctx context.Context, wf *workflow.Workflow) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 提取并保存所有函数元数据
	if err := r.extractAndSaveFunctionMetasInTx(ctx, tx, wf); err != nil {
		return err
	}

	// 2. 保存Workflow定义
	if err := r.saveWorkflowInTx(ctx, tx, wf); err != nil {
		return err
	}

	// 3. 删除旧的Task定义
	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = ?`
	if _, err := tx.ExecContext(ctx, deleteTaskDefSQL, wf.GetID()); err != nil {
		return fmt.Errorf("删除旧Task定义失败: %w", err)
	}

	// 4. 保存新的Task定义
	tasks := wf.GetTasks()
	for _, t := range tasks {
		if err := r.saveTaskDefinitionInTx(ctx, tx, wf.GetID(), t); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// saveWorkflowInTx 在事务中保存Workflow定义
func (r *WorkflowAggregateRepo) saveWorkflowInTx(ctx context.Context, tx *sqlx.Tx, wf *workflow.Workflow) error {
	depsJSON, err := json.Marshal(wf.GetDependencies())
	if err != nil {
		return fmt.Errorf("序列化依赖关系失败: %w", err)
	}

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
		MaxConcurrentTask:     wf.GetMaxConcurrentTask(),
		CronExpr:              wf.GetCronExpr(),
		CronEnabled:           wf.IsCronEnabled(),
	}

	// 先检查是否存在相同name或id的workflow（幂等性：根据name或id排除重复）
	var existingID string
	checkQuery := `SELECT id FROM workflow_definition WHERE name = ? OR id = ? LIMIT 1`
	if err := tx.GetContext(ctx, &existingID, checkQuery, wf.GetName(), wf.GetID()); err != nil {
		// 如果不存在，existingID保持为空字符串，将使用新的id插入
		if err.Error() != "sql: no rows in result set" {
			return fmt.Errorf("检查Workflow是否存在失败: %w", err)
		}
	}

	// 如果找到已存在的记录，使用其id进行更新（幂等性）
	if existingID != "" {
		workflowDAO.ID = existingID
	}

	// 使用ON CONFLICT处理id和name的唯一性约束
	// 如果id冲突，更新记录；如果name冲突（但id不同），也会通过先查询找到existingID并更新
	query := `
	INSERT INTO workflow_definition 
	(id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, 
	 transactional, transaction_mode, max_concurrent_task, cron_expr, cron_enabled)
	VALUES (:id, :name, :description, :params, :dependencies, :create_time, :status, :sub_task_error_tolerance,
	 :transactional, :transaction_mode, :max_concurrent_task, :cron_expr, :cron_enabled)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		description = excluded.description,
		params = excluded.params,
		dependencies = excluded.dependencies,
		status = excluded.status,
		sub_task_error_tolerance = excluded.sub_task_error_tolerance,
		transactional = excluded.transactional,
		transaction_mode = excluded.transaction_mode,
		max_concurrent_task = excluded.max_concurrent_task,
		cron_expr = excluded.cron_expr,
		cron_enabled = excluded.cron_enabled
	`
	if _, err := tx.NamedExecContext(ctx, query, workflowDAO); err != nil {
		// 如果插入失败且是name唯一性约束冲突（并发情况下可能发生），重新查询并更新
		if errStr := err.Error(); errStr != "" && (errStr == "UNIQUE constraint failed: workflow_definition.name" ||
			errStr == "UNIQUE constraint failed: workflow_definition.name (19)") {
			// name冲突，需要先获取该name对应的id
			var nameConflictID string
			if err := tx.GetContext(ctx, &nameConflictID, `SELECT id FROM workflow_definition WHERE name = ?`, wf.GetName()); err != nil {
				return fmt.Errorf("获取name冲突的Workflow ID失败: %w", err)
			}
			workflowDAO.ID = nameConflictID
			// 使用获取到的id重新插入
			if _, err := tx.NamedExecContext(ctx, query, workflowDAO); err != nil {
				return fmt.Errorf("保存Workflow定义失败: %w", err)
			}
		} else {
			return fmt.Errorf("保存Workflow定义失败: %w", err)
		}
	}

	return nil
}

// saveTaskDefinitionInTx 在事务中保存Task定义
func (r *WorkflowAggregateRepo) saveTaskDefinitionInTx(ctx context.Context, tx *sqlx.Tx, workflowID string, t workflow.Task) error {
	paramsJSON, err := json.Marshal(t.GetParams())
	if err != nil {
		return fmt.Errorf("序列化Task参数失败: %w", err)
	}

	depsJSON, err := json.Marshal(t.GetDependencies())
	if err != nil {
		return fmt.Errorf("序列化Task依赖失败: %w", err)
	}

	taskObj, ok := t.(*task.Task)
	if !ok {
		return fmt.Errorf("Task类型断言失败")
	}

	requiredParamsJSON, err := json.Marshal(taskObj.GetRequiredParams())
	if err != nil {
		return fmt.Errorf("序列化必需参数失败: %w", err)
	}

	resultMappingJSON, err := json.Marshal(taskObj.GetResultMapping())
	if err != nil {
		return fmt.Errorf("序列化结果映射失败: %w", err)
	}

	statusHandlersJSON, err := json.Marshal(taskObj.GetStatusHandlers())
	if err != nil {
		return fmt.Errorf("序列化状态处理器失败: %w", err)
	}

	taskDefDAO := &dao.TaskDefinitionDAO{
		ID:                   taskObj.GetID(),
		WorkflowID:           workflowID,
		Name:                 taskObj.GetName(),
		Description:          taskObj.GetDescription(),
		JobFuncName:          taskObj.GetJobFuncName(),
		CompensationFuncName: taskObj.GetCompensationFuncName(),
		Params:               string(paramsJSON),
		TimeoutSeconds:       taskObj.GetTimeoutSeconds(),
		RetryCount:           taskObj.GetRetryCount(),
		Dependencies:         string(depsJSON),
		RequiredParams:       string(requiredParamsJSON),
		ResultMapping:        string(resultMappingJSON),
		StatusHandlers:       string(statusHandlersJSON),
		IsTemplate:           taskObj.IsTemplate(),
		CreateTime:           taskObj.GetCreateTime(),
	}

	if taskObj.GetJobFuncID() != "" {
		taskDefDAO.JobFuncID.Valid = true
		taskDefDAO.JobFuncID.String = taskObj.GetJobFuncID()
	}
	if taskObj.GetCompensationFuncID() != "" {
		taskDefDAO.CompensationFuncID.Valid = true
		taskDefDAO.CompensationFuncID.String = taskObj.GetCompensationFuncID()
	}

	// 先检查是否存在相同name的task（幂等性：优先根据name排除重复，防止重复插入）
	// 优先检查(workflow_id, name)组合，如果不存在再检查id
	var existingID string
	checkQuery := `SELECT id FROM task_definition WHERE (workflow_id = ? AND name = ?) OR id = ? ORDER BY CASE WHEN workflow_id = ? AND name = ? THEN 1 ELSE 2 END LIMIT 1`
	if err := tx.GetContext(ctx, &existingID, checkQuery, workflowID, taskObj.GetName(), taskObj.GetID(), workflowID, taskObj.GetName()); err != nil {
		// 如果不存在，existingID保持为空字符串，将使用新的id插入
		if err.Error() != "sql: no rows in result set" {
			return fmt.Errorf("检查Task定义是否存在失败: %w", err)
		}
	}

	// 如果找到已存在的记录，使用其id进行更新（幂等性）
	if existingID != "" {
		taskDefDAO.ID = existingID
	}

	// 使用ON CONFLICT处理id和(workflow_id, name)的唯一性约束
	query := `
	INSERT INTO task_definition
	(id, workflow_id, name, description, job_func_id, job_func_name, compensation_func_id, compensation_func_name,
	 params, timeout_seconds, retry_count, dependencies, required_params, result_mapping, status_handlers, is_template, create_time)
	VALUES (:id, :workflow_id, :name, :description, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :timeout_seconds, :retry_count, :dependencies, :required_params, :result_mapping, :status_handlers, :is_template, :create_time)
	ON CONFLICT(id) DO UPDATE SET
		workflow_id = excluded.workflow_id,
		name = excluded.name,
		description = excluded.description,
		job_func_id = excluded.job_func_id,
		job_func_name = excluded.job_func_name,
		compensation_func_id = excluded.compensation_func_id,
		compensation_func_name = excluded.compensation_func_name,
		params = excluded.params,
		timeout_seconds = excluded.timeout_seconds,
		retry_count = excluded.retry_count,
		dependencies = excluded.dependencies,
		required_params = excluded.required_params,
		result_mapping = excluded.result_mapping,
		status_handlers = excluded.status_handlers,
		is_template = excluded.is_template
	`
	if _, err := tx.NamedExecContext(ctx, query, taskDefDAO); err != nil {
		// 如果插入失败且是(workflow_id, name)唯一性约束冲突（并发情况下可能发生），重新查询并更新
		if errStr := err.Error(); errStr != "" && (errStr == "UNIQUE constraint failed: task_definition.workflow_id, task_definition.name" ||
			errStr == "UNIQUE constraint failed: task_definition.workflow_id, task_definition.name (19)") {
			// (workflow_id, name)冲突，需要先获取该记录对应的id
			var conflictID string
			if err := tx.GetContext(ctx, &conflictID, `SELECT id FROM task_definition WHERE workflow_id = ? AND name = ?`, workflowID, taskObj.GetName()); err != nil {
				return fmt.Errorf("获取(workflow_id, name)冲突的Task ID失败: %w", err)
			}
			taskDefDAO.ID = conflictID
			// 使用获取到的id重新插入
			if _, err := tx.NamedExecContext(ctx, query, taskDefDAO); err != nil {
				return fmt.Errorf("保存Task定义失败: %w", err)
			}
		} else {
			return fmt.Errorf("保存Task定义失败: %w", err)
		}
	}

	return nil
}

// GetWorkflow 根据ID获取Workflow（不含Task定义）
func (r *WorkflowAggregateRepo) GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error) {
	var wfDAO dao.WorkflowDAO
	query := `SELECT id, name, description, params, dependencies, create_time, status, 
	          sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task, 
	          cron_expr, cron_enabled FROM workflow_definition WHERE id = ?`
	err := r.db.GetContext(ctx, &wfDAO, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询Workflow失败: %w", err)
	}

	return r.daoToWorkflow(&wfDAO)
}

// GetWorkflowWithTasks 根据ID获取Workflow及其所有Task定义
func (r *WorkflowAggregateRepo) GetWorkflowWithTasks(ctx context.Context, id string) (*workflow.Workflow, error) {
	wf, err := r.GetWorkflow(ctx, id)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, nil
	}

	taskDefs, err := r.getTaskDefinitions(ctx, id)
	if err != nil {
		return nil, err
	}

	// 使用拓扑排序确保依赖顺序
	taskMap := make(map[string]*task.Task)
	for _, td := range taskDefs {
		taskMap[td.Name] = td
	}

	added := make(map[string]bool)
	for len(added) < len(taskMap) {
		progress := false
		for name, t := range taskMap {
			if added[name] {
				continue
			}
			canAdd := true
			for _, dep := range t.GetDependencies() {
				if !added[dep] {
					canAdd = false
					break
				}
			}
			if canAdd {
				if err := wf.AddTask(t); err != nil {
					return nil, fmt.Errorf("添加Task %s 到Workflow失败: %w", name, err)
				}
				added[name] = true
				progress = true
			}
		}
		if !progress && len(added) < len(taskMap) {
			return nil, fmt.Errorf("检测到循环依赖")
		}
	}

	return wf, nil
}

// getTaskDefinitions 获取Workflow的所有Task定义
func (r *WorkflowAggregateRepo) getTaskDefinitions(ctx context.Context, workflowID string) ([]*task.Task, error) {
	var taskDAOs []dao.TaskDefinitionDAO
	query := `SELECT id, workflow_id, name, description, job_func_id, job_func_name, 
	          compensation_func_id, compensation_func_name, params, timeout_seconds, retry_count,
	          dependencies, required_params, result_mapping, status_handlers, is_template, create_time
	          FROM task_definition WHERE workflow_id = ?`
	if err := r.db.SelectContext(ctx, &taskDAOs, query, workflowID); err != nil {
		return nil, fmt.Errorf("查询Task定义失败: %w", err)
	}

	tasks := make([]*task.Task, 0, len(taskDAOs))
	for _, td := range taskDAOs {
		t, err := r.taskDefDAOToTask(&td)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}

// taskDefDAOToTask 将TaskDefinitionDAO转换为Task实体
func (r *WorkflowAggregateRepo) taskDefDAOToTask(td *dao.TaskDefinitionDAO) (*task.Task, error) {
	var params map[string]any
	if td.Params != "" {
		if err := json.Unmarshal([]byte(td.Params), &params); err != nil {
			return nil, fmt.Errorf("反序列化参数失败: %w", err)
		}
	}

	var deps []string
	if td.Dependencies != "" {
		if err := json.Unmarshal([]byte(td.Dependencies), &deps); err != nil {
			return nil, fmt.Errorf("反序列化依赖失败: %w", err)
		}
	}

	var requiredParams []string
	if td.RequiredParams != "" {
		if err := json.Unmarshal([]byte(td.RequiredParams), &requiredParams); err != nil {
			return nil, fmt.Errorf("反序列化必需参数失败: %w", err)
		}
	}

	var resultMapping map[string]string
	if td.ResultMapping != "" {
		if err := json.Unmarshal([]byte(td.ResultMapping), &resultMapping); err != nil {
			return nil, fmt.Errorf("反序列化结果映射失败: %w", err)
		}
	}

	var statusHandlers map[string][]string
	if td.StatusHandlers != "" {
		if err := json.Unmarshal([]byte(td.StatusHandlers), &statusHandlers); err != nil {
			return nil, fmt.Errorf("反序列化状态处理器失败: %w", err)
		}
	}

	t := task.NewTask(td.Name, td.Description, "", params, statusHandlers)
	t.SetID(td.ID)
	t.SetJobFuncName(td.JobFuncName)
	t.SetCompensationFuncName(td.CompensationFuncName)
	t.SetTimeoutSeconds(td.TimeoutSeconds)
	t.SetRetryCount(td.RetryCount)
	t.SetDependencies(deps)
	t.SetRequiredParams(requiredParams)
	t.SetResultMapping(resultMapping)
	t.SetTemplate(td.IsTemplate)
	t.SetCreateTime(td.CreateTime)

	if td.JobFuncID.Valid {
		t.SetJobFuncID(td.JobFuncID.String)
	}
	if td.CompensationFuncID.Valid {
		t.SetCompensationFuncID(td.CompensationFuncID.String)
	}

	return t, nil
}

// daoToWorkflow 将WorkflowDAO转换为Workflow实体
func (r *WorkflowAggregateRepo) daoToWorkflow(wfDAO *dao.WorkflowDAO) (*workflow.Workflow, error) {
	var deps map[string][]string
	if wfDAO.Dependencies != "" {
		if err := json.Unmarshal([]byte(wfDAO.Dependencies), &deps); err != nil {
			return nil, fmt.Errorf("反序列化依赖关系失败: %w", err)
		}
	}

	var params map[string]string
	if wfDAO.Params != "" {
		if err := json.Unmarshal([]byte(wfDAO.Params), &params); err != nil {
			return nil, fmt.Errorf("反序列化参数失败: %w", err)
		}
	}

	wf := workflow.NewWorkflow(wfDAO.Name, wfDAO.Description)
	wf.ID = wfDAO.ID
	wf.CreateTime = wfDAO.CreateTime
	wf.SetStatus(wfDAO.Status)

	for k, v := range deps {
		wf.Dependencies.Store(k, v)
	}
	for k, v := range params {
		wf.Params.Store(k, v)
	}

	if err := wf.SetSubTaskErrorTolerance(wfDAO.SubTaskErrorTolerance); err != nil {
		return nil, err
	}
	if err := wf.SetTransactional(wfDAO.Transactional); err != nil {
		return nil, err
	}
	wf.SetTransactionMode(wfDAO.TransactionMode)
	if err := wf.SetMaxConcurrentTask(wfDAO.MaxConcurrentTask); err != nil {
		return nil, err
	}
	if err := wf.SetCronExpr(wfDAO.CronExpr); err != nil {
		return nil, err
	}
	wf.SetCronEnabled(wfDAO.CronEnabled)

	return wf, nil
}

// DeleteWorkflow 删除Workflow及其所有关联数据（事务）
func (r *WorkflowAggregateRepo) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 获取所有WorkflowInstance ID
	var instanceIDs []string
	query := `SELECT id FROM workflow_instance WHERE workflow_id = ?`
	if err := tx.SelectContext(ctx, &instanceIDs, query, id); err != nil {
		return fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	// 2. 删除所有TaskInstance
	for _, instID := range instanceIDs {
		deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = ?`
		if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instID); err != nil {
			return fmt.Errorf("删除TaskInstance失败: %w", err)
		}
	}

	// 3. 删除所有WorkflowInstance
	deleteInstSQL := `DELETE FROM workflow_instance WHERE workflow_id = ?`
	if _, err := tx.ExecContext(ctx, deleteInstSQL, id); err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}

	// 4. 删除Task定义
	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = ?`
	if _, err := tx.ExecContext(ctx, deleteTaskDefSQL, id); err != nil {
		return fmt.Errorf("删除Task定义失败: %w", err)
	}

	// 5. 删除Workflow定义
	deleteWfSQL := `DELETE FROM workflow_definition WHERE id = ?`
	if _, err := tx.ExecContext(ctx, deleteWfSQL, id); err != nil {
		return fmt.Errorf("删除Workflow定义失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// ListWorkflows 列出所有Workflow（不含Task定义）
func (r *WorkflowAggregateRepo) ListWorkflows(ctx context.Context) ([]*workflow.Workflow, error) {
	var wfDAOs []dao.WorkflowDAO
	query := `SELECT id, name, description, params, dependencies, create_time, status, 
	          sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task, 
	          cron_expr, cron_enabled FROM workflow_definition`
	if err := r.db.SelectContext(ctx, &wfDAOs, query); err != nil {
		return nil, fmt.Errorf("查询Workflow列表失败: %w", err)
	}

	workflows := make([]*workflow.Workflow, 0, len(wfDAOs))
	for _, wfDAO := range wfDAOs {
		wf, err := r.daoToWorkflow(&wfDAO)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, wf)
	}

	return workflows, nil
}

// ========== WorkflowInstance相关操作 ==========

// StartWorkflow 启动Workflow，创建WorkflowInstance和关联的TaskInstance（事务）
func (r *WorkflowAggregateRepo) StartWorkflow(ctx context.Context, wf *workflow.Workflow) (*workflow.WorkflowInstance, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	instance := &workflow.WorkflowInstance{
		ID:         uuid.NewString(),
		WorkflowID: wf.GetID(),
		Status:     "Ready",
		StartTime:  now,
		CreateTime: now,
	}

	instanceDAO := &dao.WorkflowInstanceDAO{
		ID:         instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     instance.Status,
		CreateTime: instance.CreateTime,
	}
	instanceDAO.StartTime.Valid = true
	instanceDAO.StartTime.Time = instance.StartTime

	insertInstSQL := `
	INSERT INTO workflow_instance (id, workflow_id, status, start_time, create_time)
	VALUES (:id, :workflow_id, :status, :start_time, :create_time)
	`
	if _, err := tx.NamedExecContext(ctx, insertInstSQL, instanceDAO); err != nil {
		return nil, fmt.Errorf("创建WorkflowInstance失败: %w", err)
	}

	tasks := wf.GetTasks()
	for _, t := range tasks {
		taskInst := r.taskToTaskInstance(t, instance.ID)
		if err := r.saveTaskInstanceInTx(ctx, tx, taskInst); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}

	return instance, nil
}

// taskToTaskInstance 将Task定义转换为TaskInstance
func (r *WorkflowAggregateRepo) taskToTaskInstance(t workflow.Task, instanceID string) *storage.TaskInstance {
	taskObj, ok := t.(*task.Task)
	if !ok {
		return nil
	}

	return &storage.TaskInstance{
		ID:                   taskObj.GetID(),
		Name:                 taskObj.GetName(),
		WorkflowInstanceID:   instanceID,
		JobFuncID:            taskObj.GetJobFuncID(),
		JobFuncName:          taskObj.GetJobFuncName(),
		CompensationFuncID:   taskObj.GetCompensationFuncID(),
		CompensationFuncName: taskObj.GetCompensationFuncName(),
		Params:               taskObj.GetParams(),
		Status:               "Pending",
		TimeoutSeconds:       taskObj.GetTimeoutSeconds(),
		RetryCount:           taskObj.GetRetryCount(),
		CreateTime:           time.Now(),
	}
}

// saveTaskInstanceInTx 在事务中保存TaskInstance
func (r *WorkflowAggregateRepo) saveTaskInstanceInTx(ctx context.Context, tx *sqlx.Tx, taskInst *storage.TaskInstance) error {
	paramsJSON, err := json.Marshal(taskInst.Params)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	taskDAO := &dao.TaskDAO{
		ID:                   taskInst.ID,
		Name:                 taskInst.Name,
		WorkflowInstanceID:   taskInst.WorkflowInstanceID,
		JobFuncName:          taskInst.JobFuncName,
		CompensationFuncName: taskInst.CompensationFuncName,
		Params:               string(paramsJSON),
		Status:               taskInst.Status,
		TimeoutSeconds:       taskInst.TimeoutSeconds,
		RetryCount:           taskInst.RetryCount,
		CreateTime:           taskInst.CreateTime,
	}

	if taskInst.JobFuncID != "" {
		taskDAO.JobFuncID.Valid = true
		taskDAO.JobFuncID.String = taskInst.JobFuncID
	}
	if taskInst.CompensationFuncID != "" {
		taskDAO.CompensationFuncID.Valid = true
		taskDAO.CompensationFuncID.String = taskInst.CompensationFuncID
	}

	query := `
	INSERT OR REPLACE INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, create_time)
	VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :status, :timeout_seconds, :retry_count, :create_time)
	`
	if _, err := tx.NamedExecContext(ctx, query, taskDAO); err != nil {
		return fmt.Errorf("保存TaskInstance失败: %w", err)
	}

	return nil
}

// GetWorkflowInstance 根据ID获取WorkflowInstance
func (r *WorkflowAggregateRepo) GetWorkflowInstance(ctx context.Context, instanceID string) (*workflow.WorkflowInstance, error) {
	var instDAO dao.WorkflowInstanceDAO
	query := `SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	          FROM workflow_instance WHERE id = ?`
	if err := r.db.GetContext(ctx, &instDAO, query, instanceID); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	return r.instanceDAOToInstance(&instDAO)
}

// instanceDAOToInstance 将WorkflowInstanceDAO转换为WorkflowInstance
func (r *WorkflowAggregateRepo) instanceDAOToInstance(instDAO *dao.WorkflowInstanceDAO) (*workflow.WorkflowInstance, error) {
	instance := &workflow.WorkflowInstance{
		ID:         instDAO.ID,
		WorkflowID: instDAO.WorkflowID,
		Status:     instDAO.Status,
		CreateTime: instDAO.CreateTime,
	}

	if instDAO.StartTime.Valid {
		instance.StartTime = instDAO.StartTime.Time
	}
	if instDAO.EndTime.Valid {
		instance.EndTime = &instDAO.EndTime.Time
	}
	if instDAO.Breakpoint.Valid && instDAO.Breakpoint.String != "" {
		var breakpoint workflow.BreakpointData
		if err := json.Unmarshal([]byte(instDAO.Breakpoint.String), &breakpoint); err != nil {
			return nil, fmt.Errorf("反序列化断点数据失败: %w", err)
		}
		instance.Breakpoint = &breakpoint
	}
	if instDAO.ErrorMessage.Valid {
		instance.ErrorMessage = instDAO.ErrorMessage.String
	}

	return instance, nil
}

// GetWorkflowInstanceWithTasks 根据ID获取WorkflowInstance及其所有TaskInstance
func (r *WorkflowAggregateRepo) GetWorkflowInstanceWithTasks(ctx context.Context, instanceID string) (*workflow.WorkflowInstance, []*storage.TaskInstance, error) {
	instance, err := r.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	if instance == nil {
		return nil, nil, nil
	}

	tasks, err := r.GetTaskInstancesByWorkflowInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}

	return instance, tasks, nil
}

// UpdateWorkflowInstanceStatus 更新WorkflowInstance状态
func (r *WorkflowAggregateRepo) UpdateWorkflowInstanceStatus(ctx context.Context, instanceID string, status string) error {
	query := `UPDATE workflow_instance SET status = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, status, instanceID); err != nil {
		return fmt.Errorf("更新WorkflowInstance状态失败: %w", err)
	}
	return nil
}

// ListWorkflowInstances 根据WorkflowID列出所有WorkflowInstance
func (r *WorkflowAggregateRepo) ListWorkflowInstances(ctx context.Context, workflowID string) ([]*workflow.WorkflowInstance, error) {
	var instDAOs []dao.WorkflowInstanceDAO
	query := `SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	          FROM workflow_instance WHERE workflow_id = ?`
	if err := r.db.SelectContext(ctx, &instDAOs, query, workflowID); err != nil {
		return nil, fmt.Errorf("查询WorkflowInstance列表失败: %w", err)
	}

	instances := make([]*workflow.WorkflowInstance, 0, len(instDAOs))
	for _, instDAO := range instDAOs {
		inst, err := r.instanceDAOToInstance(&instDAO)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}

	return instances, nil
}

// ========== TaskInstance相关操作 ==========

// GetTaskInstance 根据ID获取TaskInstance
func (r *WorkflowAggregateRepo) GetTaskInstance(ctx context.Context, taskID string) (*storage.TaskInstance, error) {
	var taskDAO dao.TaskDAO
	query := `SELECT id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, 
	          compensation_func_name, params, status, timeout_seconds, retry_count, start_time, end_time, 
	          error_msg, create_time FROM task_instance WHERE id = ?`
	if err := r.db.GetContext(ctx, &taskDAO, query, taskID); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询TaskInstance失败: %w", err)
	}

	return r.taskDAOToTaskInstance(&taskDAO)
}

// taskDAOToTaskInstance 将TaskDAO转换为TaskInstance
func (r *WorkflowAggregateRepo) taskDAOToTaskInstance(taskDAO *dao.TaskDAO) (*storage.TaskInstance, error) {
	taskInst := &storage.TaskInstance{
		ID:                   taskDAO.ID,
		Name:                 taskDAO.Name,
		WorkflowInstanceID:   taskDAO.WorkflowInstanceID,
		JobFuncName:          taskDAO.JobFuncName,
		CompensationFuncName: taskDAO.CompensationFuncName,
		Status:               taskDAO.Status,
		TimeoutSeconds:       taskDAO.TimeoutSeconds,
		RetryCount:           taskDAO.RetryCount,
		CreateTime:           taskDAO.CreateTime,
	}

	if taskDAO.JobFuncID.Valid {
		taskInst.JobFuncID = taskDAO.JobFuncID.String
	}
	if taskDAO.CompensationFuncID.Valid {
		taskInst.CompensationFuncID = taskDAO.CompensationFuncID.String
	}
	if taskDAO.StartTime.Valid {
		taskInst.StartTime = &taskDAO.StartTime.Time
	}
	if taskDAO.EndTime.Valid {
		taskInst.EndTime = &taskDAO.EndTime.Time
	}
	if taskDAO.ErrorMessage.Valid {
		taskInst.ErrorMessage = taskDAO.ErrorMessage.String
	}

	if taskDAO.Params != "" {
		if err := json.Unmarshal([]byte(taskDAO.Params), &taskInst.Params); err != nil {
			return nil, fmt.Errorf("反序列化参数失败: %w", err)
		}
	} else {
		taskInst.Params = make(map[string]interface{})
	}

	return taskInst, nil
}

// UpdateTaskInstanceStatus 更新TaskInstance状态
func (r *WorkflowAggregateRepo) UpdateTaskInstanceStatus(ctx context.Context, taskID string, status string) error {
	query := `UPDATE task_instance SET status = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, status, taskID); err != nil {
		return fmt.Errorf("更新TaskInstance状态失败: %w", err)
	}
	return nil
}

// UpdateTaskInstanceStatusWithError 更新TaskInstance状态和错误信息
func (r *WorkflowAggregateRepo) UpdateTaskInstanceStatusWithError(ctx context.Context, taskID string, status string, errorMsg string) error {
	query := `UPDATE task_instance SET status = ?, error_msg = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, status, errorMsg, taskID); err != nil {
		return fmt.Errorf("更新TaskInstance状态和错误信息失败: %w", err)
	}
	return nil
}

// SaveTaskInstance 保存TaskInstance（用于动态添加子任务场景）
func (r *WorkflowAggregateRepo) SaveTaskInstance(ctx context.Context, taskInst *storage.TaskInstance) error {
	paramsJSON, err := json.Marshal(taskInst.Params)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	taskDAO := &dao.TaskDAO{
		ID:                   taskInst.ID,
		Name:                 taskInst.Name,
		WorkflowInstanceID:   taskInst.WorkflowInstanceID,
		JobFuncName:          taskInst.JobFuncName,
		CompensationFuncName: taskInst.CompensationFuncName,
		Params:               string(paramsJSON),
		Status:               taskInst.Status,
		TimeoutSeconds:       taskInst.TimeoutSeconds,
		RetryCount:           taskInst.RetryCount,
		CreateTime:           taskInst.CreateTime,
	}

	if taskInst.JobFuncID != "" {
		taskDAO.JobFuncID.Valid = true
		taskDAO.JobFuncID.String = taskInst.JobFuncID
	}
	if taskInst.CompensationFuncID != "" {
		taskDAO.CompensationFuncID.Valid = true
		taskDAO.CompensationFuncID.String = taskInst.CompensationFuncID
	}
	if taskInst.StartTime != nil {
		taskDAO.StartTime.Valid = true
		taskDAO.StartTime.Time = *taskInst.StartTime
	}
	if taskInst.EndTime != nil {
		taskDAO.EndTime.Valid = true
		taskDAO.EndTime.Time = *taskInst.EndTime
	}
	if taskInst.ErrorMessage != "" {
		taskDAO.ErrorMessage.Valid = true
		taskDAO.ErrorMessage.String = taskInst.ErrorMessage
	}

	query := `
	INSERT OR REPLACE INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, start_time, end_time, error_msg, create_time)
	VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :status, :timeout_seconds, :retry_count, :start_time, :end_time, :error_msg, :create_time)
	`
	if _, err := r.db.NamedExecContext(ctx, query, taskDAO); err != nil {
		return fmt.Errorf("保存TaskInstance失败: %w", err)
	}

	return nil
}

// GetTaskInstancesByWorkflowInstance 根据WorkflowInstance ID获取所有TaskInstance
func (r *WorkflowAggregateRepo) GetTaskInstancesByWorkflowInstance(ctx context.Context, instanceID string) ([]*storage.TaskInstance, error) {
	var taskDAOs []dao.TaskDAO
	query := `SELECT id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, 
	          compensation_func_name, params, status, timeout_seconds, retry_count, start_time, end_time, 
	          error_msg, create_time FROM task_instance WHERE workflow_instance_id = ?`
	if err := r.db.SelectContext(ctx, &taskDAOs, query, instanceID); err != nil {
		return nil, fmt.Errorf("查询TaskInstance列表失败: %w", err)
	}

	tasks := make([]*storage.TaskInstance, 0, len(taskDAOs))
	for _, taskDAO := range taskDAOs {
		taskInst, err := r.taskDAOToTaskInstance(&taskDAO)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, taskInst)
	}

	return tasks, nil
}

// DeleteWorkflowInstance 删除WorkflowInstance及其所有TaskInstance（事务，幂等）
// 如果Instance不存在，不会报错
func (r *WorkflowAggregateRepo) DeleteWorkflowInstance(ctx context.Context, instanceID string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 删除所有TaskInstance（幂等：不存在时不报错）
	deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = ?`
	if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instanceID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}

	// 2. 删除WorkflowInstance（幂等：不存在时不报错）
	deleteInstSQL := `DELETE FROM workflow_instance WHERE id = ?`
	if _, err := tx.ExecContext(ctx, deleteInstSQL, instanceID); err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// DeleteTaskInstance 删除TaskInstance（幂等）
// 如果TaskInstance不存在，不会报错
func (r *WorkflowAggregateRepo) DeleteTaskInstance(ctx context.Context, taskID string) error {
	query := `DELETE FROM task_instance WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, taskID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}
	// SQL DELETE对不存在的记录不会报错，天然幂等
	return nil
}

// ========== 函数元数据管理操作 ==========

// extractAndSaveFunctionMetasInTx 在事务中提取并保存所有函数元数据
func (r *WorkflowAggregateRepo) extractAndSaveFunctionMetasInTx(ctx context.Context, tx *sqlx.Tx, wf *workflow.Workflow) error {
	// 收集所有函数元数据
	jobFuncs := make(map[string]*storage.JobFunctionMeta)
	compensationFuncs := make(map[string]*storage.CompensationFunctionMeta)
	taskHandlers := make(map[string]*storage.TaskHandlerMeta)

	// 遍历所有Task，提取函数元数据
	tasks := wf.GetTasks()
	for _, t := range tasks {
		taskObj, ok := t.(*task.Task)
		if !ok {
			continue
		}

		// 提取 Job function
		if taskObj.GetJobFuncName() != "" {
			jobFuncs[taskObj.GetJobFuncName()] = &storage.JobFunctionMeta{
				ID:          taskObj.GetJobFuncID(),
				Name:        taskObj.GetJobFuncName(),
				Description: "", // Task中没有函数描述，使用空字符串
			}
		}

		// 提取 Compensation function
		if taskObj.GetCompensationFuncName() != "" {
			compensationFuncs[taskObj.GetCompensationFuncName()] = &storage.CompensationFunctionMeta{
				ID:          taskObj.GetCompensationFuncID(),
				Name:        taskObj.GetCompensationFuncName(),
				Description: "", // Task中没有函数描述，使用空字符串
			}
		}

		// 提取 Task handlers
		statusHandlers := taskObj.GetStatusHandlers()
		for _, handlers := range statusHandlers {
			for _, handlerName := range handlers {
				if handlerName != "" {
					taskHandlers[handlerName] = &storage.TaskHandlerMeta{
						Name:        handlerName,
						Description: "", // Task中没有handler描述，使用空字符串
					}
				}
			}
		}
	}

	// 在事务中保存所有Job function元数据
	for _, meta := range jobFuncs {
		if err := r.saveJobFunctionMetaInTx(ctx, tx, meta); err != nil {
			return fmt.Errorf("保存Job函数元数据失败: %w", err)
		}
	}

	// 在事务中保存所有Compensation function元数据
	for _, meta := range compensationFuncs {
		if err := r.saveCompensationFunctionMetaInTx(ctx, tx, meta); err != nil {
			return fmt.Errorf("保存补偿函数元数据失败: %w", err)
		}
	}

	// 在事务中保存所有Task handler元数据
	for _, meta := range taskHandlers {
		if err := r.saveTaskHandlerMetaInTx(ctx, tx, meta); err != nil {
			return fmt.Errorf("保存Task Handler元数据失败: %w", err)
		}
	}

	return nil
}

// saveJobFunctionMetaInTx 在事务中保存Job函数元数据（幂等）
func (r *WorkflowAggregateRepo) saveJobFunctionMetaInTx(ctx context.Context, tx *sqlx.Tx, meta *storage.JobFunctionMeta) error {
	// 如果ID为空，生成新ID
	if meta.ID == "" {
		meta.ID = uuid.NewString()
	}

	// 设置时间戳
	now := time.Now()
	if meta.CreateTime.IsZero() {
		meta.CreateTime = now
	}
	meta.UpdateTime = now

	// 构建DAO对象
	jobFuncDAO := &dao.JobFunctionDAO{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		CreateTime:  meta.CreateTime,
		UpdateTime:  meta.UpdateTime,
		// CodePath, Hash, ParamTypes 暂时为空
		CodePath:   "",
		Hash:       "",
		ParamTypes: "",
	}

	// 使用 INSERT OR REPLACE 实现幂等性
	query := `
	INSERT OR REPLACE INTO job_function_meta 
	(id, name, description, code_path, hash, param_types, create_time, update_time)
	VALUES (:id, :name, :description, :code_path, :hash, :param_types, :create_time, :update_time)
	`
	if _, err := tx.NamedExecContext(ctx, query, jobFuncDAO); err != nil {
		return fmt.Errorf("保存JobFunctionMeta失败: %w", err)
	}

	return nil
}

// SaveJobFunctionMeta 保存Job函数元数据（幂等）
func (r *WorkflowAggregateRepo) SaveJobFunctionMeta(ctx context.Context, meta *storage.JobFunctionMeta) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	if err := r.saveJobFunctionMetaInTx(ctx, tx, meta); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetJobFunctionMetaByID 根据ID获取Job函数元数据
func (r *WorkflowAggregateRepo) GetJobFunctionMetaByID(ctx context.Context, id string) (*storage.JobFunctionMeta, error) {
	var dao dao.JobFunctionDAO
	query := `SELECT id, name, description, code_path, hash, param_types, create_time, update_time 
	          FROM job_function_meta WHERE id = ?`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询JobFunctionMeta失败: %w", err)
	}

	meta := &storage.JobFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// GetJobFunctionMetaByName 根据名称获取Job函数元数据
func (r *WorkflowAggregateRepo) GetJobFunctionMetaByName(ctx context.Context, name string) (*storage.JobFunctionMeta, error) {
	var dao dao.JobFunctionDAO
	query := `SELECT id, name, description, code_path, hash, param_types, create_time, update_time 
	          FROM job_function_meta WHERE name = ?`
	err := r.db.GetContext(ctx, &dao, query, name)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询JobFunctionMeta失败: %w", err)
	}

	meta := &storage.JobFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// saveTaskHandlerMetaInTx 在事务中保存Task Handler元数据（幂等）
func (r *WorkflowAggregateRepo) saveTaskHandlerMetaInTx(ctx context.Context, tx *sqlx.Tx, meta *storage.TaskHandlerMeta) error {
	// 如果ID为空，生成新ID
	if meta.ID == "" {
		meta.ID = uuid.NewString()
	}

	// 设置时间戳
	now := time.Now()
	if meta.CreateTime.IsZero() {
		meta.CreateTime = now
	}
	meta.UpdateTime = now

	// 构建DAO对象
	taskHandlerDAO := &dao.TaskHandlerDAO{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		CreateTime:  meta.CreateTime,
		UpdateTime:  meta.UpdateTime,
	}

	// 使用 INSERT OR REPLACE 实现幂等性
	query := `
	INSERT OR REPLACE INTO task_handler_meta 
	(id, name, description, create_time, update_time)
	VALUES (:id, :name, :description, :create_time, :update_time)
	`
	if _, err := tx.NamedExecContext(ctx, query, taskHandlerDAO); err != nil {
		return fmt.Errorf("保存TaskHandlerMeta失败: %w", err)
	}

	return nil
}

// SaveTaskHandlerMeta 保存Task Handler元数据（幂等）
func (r *WorkflowAggregateRepo) SaveTaskHandlerMeta(ctx context.Context, meta *storage.TaskHandlerMeta) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	if err := r.saveTaskHandlerMetaInTx(ctx, tx, meta); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetTaskHandlerMetaByID 根据ID获取Task Handler元数据
func (r *WorkflowAggregateRepo) GetTaskHandlerMetaByID(ctx context.Context, id string) (*storage.TaskHandlerMeta, error) {
	var dao dao.TaskHandlerDAO
	query := `SELECT id, name, description, create_time, update_time 
	          FROM task_handler_meta WHERE id = ?`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询TaskHandlerMeta失败: %w", err)
	}

	meta := &storage.TaskHandlerMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// GetTaskHandlerMetaByName 根据名称获取Task Handler元数据
func (r *WorkflowAggregateRepo) GetTaskHandlerMetaByName(ctx context.Context, name string) (*storage.TaskHandlerMeta, error) {
	var dao dao.TaskHandlerDAO
	query := `SELECT id, name, description, create_time, update_time 
	          FROM task_handler_meta WHERE name = ?`
	err := r.db.GetContext(ctx, &dao, query, name)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询TaskHandlerMeta失败: %w", err)
	}

	meta := &storage.TaskHandlerMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// saveCompensationFunctionMetaInTx 在事务中保存补偿函数元数据（幂等）
func (r *WorkflowAggregateRepo) saveCompensationFunctionMetaInTx(ctx context.Context, tx *sqlx.Tx, meta *storage.CompensationFunctionMeta) error {
	// 如果ID为空，生成新ID
	if meta.ID == "" {
		meta.ID = uuid.NewString()
	}

	// 设置时间戳
	now := time.Now()
	if meta.CreateTime.IsZero() {
		meta.CreateTime = now
	}
	meta.UpdateTime = now

	// 构建DAO对象
	compensationFuncDAO := &dao.CompensationFunctionDAO{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		CreateTime:  meta.CreateTime,
		UpdateTime:  meta.UpdateTime,
	}

	// 使用 INSERT OR REPLACE 实现幂等性
	query := `
	INSERT OR REPLACE INTO compensation_function_meta 
	(id, name, description, create_time, update_time)
	VALUES (:id, :name, :description, :create_time, :update_time)
	`
	if _, err := tx.NamedExecContext(ctx, query, compensationFuncDAO); err != nil {
		return fmt.Errorf("保存CompensationFunctionMeta失败: %w", err)
	}

	return nil
}

// SaveCompensationFunctionMeta 保存补偿函数元数据（幂等）
func (r *WorkflowAggregateRepo) SaveCompensationFunctionMeta(ctx context.Context, meta *storage.CompensationFunctionMeta) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	if err := r.saveCompensationFunctionMetaInTx(ctx, tx, meta); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetCompensationFunctionMetaByID 根据ID获取补偿函数元数据
func (r *WorkflowAggregateRepo) GetCompensationFunctionMetaByID(ctx context.Context, id string) (*storage.CompensationFunctionMeta, error) {
	var dao dao.CompensationFunctionDAO
	query := `SELECT id, name, description, create_time, update_time 
	          FROM compensation_function_meta WHERE id = ?`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询CompensationFunctionMeta失败: %w", err)
	}

	meta := &storage.CompensationFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// GetCompensationFunctionMetaByName 根据名称获取补偿函数元数据
func (r *WorkflowAggregateRepo) GetCompensationFunctionMetaByName(ctx context.Context, name string) (*storage.CompensationFunctionMeta, error) {
	var dao dao.CompensationFunctionDAO
	query := `SELECT id, name, description, create_time, update_time 
	          FROM compensation_function_meta WHERE name = ?`
	err := r.db.GetContext(ctx, &dao, query, name)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询CompensationFunctionMeta失败: %w", err)
	}

	meta := &storage.CompensationFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// 确保 WorkflowAggregateRepo 实现 WorkflowAggregateRepository 接口
var _ storage.WorkflowAggregateRepository = (*WorkflowAggregateRepo)(nil)
