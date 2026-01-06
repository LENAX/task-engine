package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stevelan1995/task-engine/pkg/core/task"
	"github.com/stevelan1995/task-engine/pkg/core/workflow"
	"github.com/stevelan1995/task-engine/pkg/storage"
	"github.com/stevelan1995/task-engine/pkg/storage/dao"
)

// WorkflowAggregateRepo Workflow聚合根Repository的PostgreSQL实现（对外导出）
type WorkflowAggregateRepo struct {
	db      *sqlx.DB
	dialect *PostgresDialect
}

// NewWorkflowAggregateRepo 创建PostgreSQL Workflow聚合根Repository实例（对外导出）
func NewWorkflowAggregateRepo(db *sqlx.DB) (*WorkflowAggregateRepo, error) {
	repo := &WorkflowAggregateRepo{
		db:      db,
		dialect: NewPostgresDialect(),
	}
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	return repo, nil
}

// NewWorkflowAggregateRepoFromDSN 通过DSN创建PostgreSQL Workflow聚合根Repository实例（对外导出）
// dsn格式: postgres://user:password@host:port/dbname?sslmode=disable
func NewWorkflowAggregateRepoFromDSN(dsn string) (*WorkflowAggregateRepo, error) {
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 配置PostgreSQL
	dialect := NewPostgresDialect()
	for _, sql := range dialect.ConfigureDB() {
		if _, err := db.Exec(sql); err != nil {
			// 忽略配置错误，继续执行
		}
	}

	return NewWorkflowAggregateRepo(db)
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
		id VARCHAR(36) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		description TEXT,
		params TEXT,
		dependencies TEXT,
		create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status VARCHAR(50) NOT NULL DEFAULT 'ENABLED',
		sub_task_error_tolerance DOUBLE PRECISION NOT NULL DEFAULT 0.0,
		transactional BOOLEAN NOT NULL DEFAULT FALSE,
		transaction_mode VARCHAR(50) DEFAULT '',
		max_concurrent_task INT NOT NULL DEFAULT 10,
		cron_expr VARCHAR(100) DEFAULT '',
		cron_enabled BOOLEAN NOT NULL DEFAULT FALSE
	);
	`

	// Task定义表
	createTaskDefSQL := `
	CREATE TABLE IF NOT EXISTS task_definition (
		id VARCHAR(36) PRIMARY KEY,
		workflow_id VARCHAR(36) NOT NULL REFERENCES workflow_definition(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		description TEXT,
		job_func_id VARCHAR(36),
		job_func_name VARCHAR(255),
		compensation_func_id VARCHAR(36),
		compensation_func_name VARCHAR(255),
		params TEXT,
		timeout_seconds INT DEFAULT 30,
		retry_count INT DEFAULT 0,
		dependencies TEXT,
		required_params TEXT,
		result_mapping TEXT,
		status_handlers TEXT,
		is_template BOOLEAN DEFAULT FALSE,
		create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_task_definition_workflow_id ON task_definition(workflow_id);
	`

	// WorkflowInstance表
	createInstanceSQL := `
	CREATE TABLE IF NOT EXISTS workflow_instance (
		id VARCHAR(36) PRIMARY KEY,
		workflow_id VARCHAR(36) NOT NULL REFERENCES workflow_definition(id),
		status VARCHAR(50) NOT NULL,
		start_time TIMESTAMP,
		end_time TIMESTAMP,
		breakpoint TEXT,
		error_message TEXT,
		create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_workflow_id ON workflow_instance(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_status ON workflow_instance(status);
	`

	// TaskInstance表
	createTaskInstSQL := `
	CREATE TABLE IF NOT EXISTS task_instance (
		id VARCHAR(36) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		workflow_instance_id VARCHAR(36) NOT NULL REFERENCES workflow_instance(id) ON DELETE CASCADE,
		job_func_id VARCHAR(36),
		job_func_name VARCHAR(255),
		compensation_func_id VARCHAR(36),
		compensation_func_name VARCHAR(255),
		params TEXT,
		status VARCHAR(50) NOT NULL,
		timeout_seconds INT DEFAULT 30,
		retry_count INT DEFAULT 0,
		start_time TIMESTAMP,
		end_time TIMESTAMP,
		error_msg TEXT,
		create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_task_instance_workflow_instance_id ON task_instance(workflow_instance_id);
	CREATE INDEX IF NOT EXISTS idx_task_instance_status ON task_instance(status);
	`

	for _, sql := range []string{createWorkflowSQL, createTaskDefSQL, createInstanceSQL, createTaskInstSQL} {
		if _, err := r.db.Exec(sql); err != nil {
			// PostgreSQL会报错如果表/索引已存在，忽略这些错误
			if !strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("执行SQL失败: %w", err)
			}
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

	if err := r.saveWorkflowInTx(ctx, tx, wf); err != nil {
		return err
	}

	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = $1`
	if _, err := tx.ExecContext(ctx, deleteTaskDefSQL, wf.GetID()); err != nil {
		return fmt.Errorf("删除旧Task定义失败: %w", err)
	}

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

func (r *WorkflowAggregateRepo) saveWorkflowInTx(ctx context.Context, tx *sqlx.Tx, wf *workflow.Workflow) error {
	depsJSON, _ := json.Marshal(wf.GetDependencies())

	paramsMap := make(map[string]string)
	wf.Params.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(string); ok {
				paramsMap[k] = v
			}
		}
		return true
	})
	paramsJSON, _ := json.Marshal(paramsMap)

	query := `
	INSERT INTO workflow_definition 
	(id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, 
	 transactional, transaction_mode, max_concurrent_task, cron_expr, cron_enabled)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	ON CONFLICT (id) DO UPDATE SET
	 name = EXCLUDED.name, description = EXCLUDED.description, params = EXCLUDED.params,
	 dependencies = EXCLUDED.dependencies, status = EXCLUDED.status,
	 sub_task_error_tolerance = EXCLUDED.sub_task_error_tolerance,
	 transactional = EXCLUDED.transactional, transaction_mode = EXCLUDED.transaction_mode,
	 max_concurrent_task = EXCLUDED.max_concurrent_task, cron_expr = EXCLUDED.cron_expr,
	 cron_enabled = EXCLUDED.cron_enabled
	`
	if _, err := tx.ExecContext(ctx, query,
		wf.GetID(), wf.GetName(), wf.Description, string(paramsJSON), string(depsJSON),
		wf.CreateTime, wf.GetStatus(), wf.GetSubTaskErrorTolerance(),
		wf.GetTransactional(), wf.GetTransactionMode(), wf.GetMaxConcurrentTask(),
		wf.GetCronExpr(), wf.IsCronEnabled()); err != nil {
		return fmt.Errorf("保存Workflow定义失败: %w", err)
	}

	return nil
}

func (r *WorkflowAggregateRepo) saveTaskDefinitionInTx(ctx context.Context, tx *sqlx.Tx, workflowID string, t workflow.Task) error {
	paramsJSON, _ := json.Marshal(t.GetParams())
	depsJSON, _ := json.Marshal(t.GetDependencies())

	taskObj, ok := t.(*task.Task)
	if !ok {
		return fmt.Errorf("Task类型断言失败")
	}

	requiredParamsJSON, _ := json.Marshal(taskObj.GetRequiredParams())
	resultMappingJSON, _ := json.Marshal(taskObj.GetResultMapping())
	statusHandlersJSON, _ := json.Marshal(taskObj.GetStatusHandlers())

	var jobFuncID, compensationFuncID *string
	if taskObj.GetJobFuncID() != "" {
		id := taskObj.GetJobFuncID()
		jobFuncID = &id
	}
	if taskObj.GetCompensationFuncID() != "" {
		id := taskObj.GetCompensationFuncID()
		compensationFuncID = &id
	}

	query := `
	INSERT INTO task_definition
	(id, workflow_id, name, description, job_func_id, job_func_name, compensation_func_id, compensation_func_name,
	 params, timeout_seconds, retry_count, dependencies, required_params, result_mapping, status_handlers, is_template, create_time)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	ON CONFLICT (id) DO UPDATE SET
	 name = EXCLUDED.name, description = EXCLUDED.description, job_func_id = EXCLUDED.job_func_id,
	 job_func_name = EXCLUDED.job_func_name, compensation_func_id = EXCLUDED.compensation_func_id,
	 compensation_func_name = EXCLUDED.compensation_func_name, params = EXCLUDED.params,
	 timeout_seconds = EXCLUDED.timeout_seconds, retry_count = EXCLUDED.retry_count,
	 dependencies = EXCLUDED.dependencies, required_params = EXCLUDED.required_params,
	 result_mapping = EXCLUDED.result_mapping, status_handlers = EXCLUDED.status_handlers,
	 is_template = EXCLUDED.is_template
	`
	if _, err := tx.ExecContext(ctx, query,
		taskObj.GetID(), workflowID, taskObj.GetName(), taskObj.GetDescription(),
		jobFuncID, taskObj.GetJobFuncName(), compensationFuncID, taskObj.GetCompensationFuncName(),
		string(paramsJSON), taskObj.GetTimeoutSeconds(), taskObj.GetRetryCount(),
		string(depsJSON), string(requiredParamsJSON), string(resultMappingJSON),
		string(statusHandlersJSON), taskObj.IsTemplate(), taskObj.GetCreateTime()); err != nil {
		return fmt.Errorf("保存Task定义失败: %w", err)
	}

	return nil
}

// GetWorkflow 根据ID获取Workflow（不含Task定义）
func (r *WorkflowAggregateRepo) GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error) {
	var wfDAO dao.WorkflowDAO
	query := `SELECT id, name, description, params, dependencies, create_time, status, 
	          sub_task_error_tolerance, transactional, transaction_mode, max_concurrent_task, 
	          cron_expr, cron_enabled FROM workflow_definition WHERE id = $1`
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
	if err != nil || wf == nil {
		return wf, err
	}

	taskDefs, err := r.getTaskDefinitions(ctx, id)
	if err != nil {
		return nil, err
	}

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

func (r *WorkflowAggregateRepo) getTaskDefinitions(ctx context.Context, workflowID string) ([]*task.Task, error) {
	var taskDAOs []dao.TaskDefinitionDAO
	query := `SELECT id, workflow_id, name, description, job_func_id, job_func_name, 
	          compensation_func_id, compensation_func_name, params, timeout_seconds, retry_count,
	          dependencies, required_params, result_mapping, status_handlers, is_template, create_time
	          FROM task_definition WHERE workflow_id = $1`
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

func (r *WorkflowAggregateRepo) taskDefDAOToTask(td *dao.TaskDefinitionDAO) (*task.Task, error) {
	var params map[string]any
	if td.Params != "" {
		json.Unmarshal([]byte(td.Params), &params)
	}

	var deps []string
	if td.Dependencies != "" {
		json.Unmarshal([]byte(td.Dependencies), &deps)
	}

	var requiredParams []string
	if td.RequiredParams != "" {
		json.Unmarshal([]byte(td.RequiredParams), &requiredParams)
	}

	var resultMapping map[string]string
	if td.ResultMapping != "" {
		json.Unmarshal([]byte(td.ResultMapping), &resultMapping)
	}

	var statusHandlers map[string][]string
	if td.StatusHandlers != "" {
		json.Unmarshal([]byte(td.StatusHandlers), &statusHandlers)
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

func (r *WorkflowAggregateRepo) daoToWorkflow(wfDAO *dao.WorkflowDAO) (*workflow.Workflow, error) {
	var deps map[string][]string
	if wfDAO.Dependencies != "" {
		json.Unmarshal([]byte(wfDAO.Dependencies), &deps)
	}

	var params map[string]string
	if wfDAO.Params != "" {
		json.Unmarshal([]byte(wfDAO.Params), &params)
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

	wf.SetSubTaskErrorTolerance(wfDAO.SubTaskErrorTolerance)
	wf.SetTransactional(wfDAO.Transactional)
	wf.SetTransactionMode(wfDAO.TransactionMode)
	wf.SetMaxConcurrentTask(wfDAO.MaxConcurrentTask)
	wf.SetCronExpr(wfDAO.CronExpr)
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

	var instanceIDs []string
	query := `SELECT id FROM workflow_instance WHERE workflow_id = $1`
	if err := tx.SelectContext(ctx, &instanceIDs, query, id); err != nil {
		return fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	for _, instID := range instanceIDs {
		deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = $1`
		if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instID); err != nil {
			return fmt.Errorf("删除TaskInstance失败: %w", err)
		}
	}

	deleteInstSQL := `DELETE FROM workflow_instance WHERE workflow_id = $1`
	if _, err := tx.ExecContext(ctx, deleteInstSQL, id); err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}

	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = $1`
	if _, err := tx.ExecContext(ctx, deleteTaskDefSQL, id); err != nil {
		return fmt.Errorf("删除Task定义失败: %w", err)
	}

	deleteWfSQL := `DELETE FROM workflow_definition WHERE id = $1`
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
		wf, _ := r.daoToWorkflow(&wfDAO)
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

	insertInstSQL := `
	INSERT INTO workflow_instance (id, workflow_id, status, start_time, create_time)
	VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := tx.ExecContext(ctx, insertInstSQL,
		instance.ID, instance.WorkflowID, instance.Status, instance.StartTime, instance.CreateTime); err != nil {
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

func (r *WorkflowAggregateRepo) saveTaskInstanceInTx(ctx context.Context, tx *sqlx.Tx, taskInst *storage.TaskInstance) error {
	paramsJSON, _ := json.Marshal(taskInst.Params)

	var jobFuncID, compensationFuncID *string
	if taskInst.JobFuncID != "" {
		jobFuncID = &taskInst.JobFuncID
	}
	if taskInst.CompensationFuncID != "" {
		compensationFuncID = &taskInst.CompensationFuncID
	}

	query := `
	INSERT INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, create_time)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT (id) DO UPDATE SET
	 name = EXCLUDED.name, job_func_id = EXCLUDED.job_func_id, job_func_name = EXCLUDED.job_func_name,
	 compensation_func_id = EXCLUDED.compensation_func_id, compensation_func_name = EXCLUDED.compensation_func_name,
	 params = EXCLUDED.params, status = EXCLUDED.status, timeout_seconds = EXCLUDED.timeout_seconds,
	 retry_count = EXCLUDED.retry_count
	`
	if _, err := tx.ExecContext(ctx, query,
		taskInst.ID, taskInst.Name, taskInst.WorkflowInstanceID,
		jobFuncID, taskInst.JobFuncName, compensationFuncID, taskInst.CompensationFuncName,
		string(paramsJSON), taskInst.Status, taskInst.TimeoutSeconds, taskInst.RetryCount,
		taskInst.CreateTime); err != nil {
		return fmt.Errorf("保存TaskInstance失败: %w", err)
	}

	return nil
}

// GetWorkflowInstance 根据ID获取WorkflowInstance
func (r *WorkflowAggregateRepo) GetWorkflowInstance(ctx context.Context, instanceID string) (*workflow.WorkflowInstance, error) {
	var instDAO dao.WorkflowInstanceDAO
	query := `SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	          FROM workflow_instance WHERE id = $1`
	if err := r.db.GetContext(ctx, &instDAO, query, instanceID); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	return r.instanceDAOToInstance(&instDAO)
}

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
		json.Unmarshal([]byte(instDAO.Breakpoint.String), &breakpoint)
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
	if err != nil || instance == nil {
		return instance, nil, err
	}

	tasks, err := r.GetTaskInstancesByWorkflowInstance(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}

	return instance, tasks, nil
}

// UpdateWorkflowInstanceStatus 更新WorkflowInstance状态
func (r *WorkflowAggregateRepo) UpdateWorkflowInstanceStatus(ctx context.Context, instanceID string, status string) error {
	query := `UPDATE workflow_instance SET status = $1 WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, query, status, instanceID); err != nil {
		return fmt.Errorf("更新WorkflowInstance状态失败: %w", err)
	}
	return nil
}

// ListWorkflowInstances 根据WorkflowID列出所有WorkflowInstance
func (r *WorkflowAggregateRepo) ListWorkflowInstances(ctx context.Context, workflowID string) ([]*workflow.WorkflowInstance, error) {
	var instDAOs []dao.WorkflowInstanceDAO
	query := `SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	          FROM workflow_instance WHERE workflow_id = $1`
	if err := r.db.SelectContext(ctx, &instDAOs, query, workflowID); err != nil {
		return nil, fmt.Errorf("查询WorkflowInstance列表失败: %w", err)
	}

	instances := make([]*workflow.WorkflowInstance, 0, len(instDAOs))
	for _, instDAO := range instDAOs {
		inst, _ := r.instanceDAOToInstance(&instDAO)
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
	          error_msg, create_time FROM task_instance WHERE id = $1`
	if err := r.db.GetContext(ctx, &taskDAO, query, taskID); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询TaskInstance失败: %w", err)
	}

	return r.taskDAOToTaskInstance(&taskDAO)
}

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
		json.Unmarshal([]byte(taskDAO.Params), &taskInst.Params)
	} else {
		taskInst.Params = make(map[string]interface{})
	}

	return taskInst, nil
}

// UpdateTaskInstanceStatus 更新TaskInstance状态
func (r *WorkflowAggregateRepo) UpdateTaskInstanceStatus(ctx context.Context, taskID string, status string) error {
	query := `UPDATE task_instance SET status = $1 WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, query, status, taskID); err != nil {
		return fmt.Errorf("更新TaskInstance状态失败: %w", err)
	}
	return nil
}

// UpdateTaskInstanceStatusWithError 更新TaskInstance状态和错误信息
func (r *WorkflowAggregateRepo) UpdateTaskInstanceStatusWithError(ctx context.Context, taskID string, status string, errorMsg string) error {
	query := `UPDATE task_instance SET status = $1, error_msg = $2 WHERE id = $3`
	if _, err := r.db.ExecContext(ctx, query, status, errorMsg, taskID); err != nil {
		return fmt.Errorf("更新TaskInstance状态和错误信息失败: %w", err)
	}
	return nil
}

// SaveTaskInstance 保存TaskInstance
func (r *WorkflowAggregateRepo) SaveTaskInstance(ctx context.Context, taskInst *storage.TaskInstance) error {
	paramsJSON, _ := json.Marshal(taskInst.Params)

	var jobFuncID, compensationFuncID, errorMsg *string
	var startTime, endTime *time.Time
	if taskInst.JobFuncID != "" {
		jobFuncID = &taskInst.JobFuncID
	}
	if taskInst.CompensationFuncID != "" {
		compensationFuncID = &taskInst.CompensationFuncID
	}
	if taskInst.ErrorMessage != "" {
		errorMsg = &taskInst.ErrorMessage
	}
	startTime = taskInst.StartTime
	endTime = taskInst.EndTime

	query := `
	INSERT INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, start_time, end_time, error_msg, create_time)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	ON CONFLICT (id) DO UPDATE SET
	 name = EXCLUDED.name, job_func_id = EXCLUDED.job_func_id, job_func_name = EXCLUDED.job_func_name,
	 compensation_func_id = EXCLUDED.compensation_func_id, compensation_func_name = EXCLUDED.compensation_func_name,
	 params = EXCLUDED.params, status = EXCLUDED.status, timeout_seconds = EXCLUDED.timeout_seconds,
	 retry_count = EXCLUDED.retry_count, start_time = EXCLUDED.start_time, end_time = EXCLUDED.end_time,
	 error_msg = EXCLUDED.error_msg
	`
	if _, err := r.db.ExecContext(ctx, query,
		taskInst.ID, taskInst.Name, taskInst.WorkflowInstanceID,
		jobFuncID, taskInst.JobFuncName, compensationFuncID, taskInst.CompensationFuncName,
		string(paramsJSON), taskInst.Status, taskInst.TimeoutSeconds, taskInst.RetryCount,
		startTime, endTime, errorMsg, taskInst.CreateTime); err != nil {
		return fmt.Errorf("保存TaskInstance失败: %w", err)
	}

	return nil
}

// GetTaskInstancesByWorkflowInstance 根据WorkflowInstance ID获取所有TaskInstance
func (r *WorkflowAggregateRepo) GetTaskInstancesByWorkflowInstance(ctx context.Context, instanceID string) ([]*storage.TaskInstance, error) {
	var taskDAOs []dao.TaskDAO
	query := `SELECT id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, 
	          compensation_func_name, params, status, timeout_seconds, retry_count, start_time, end_time, 
	          error_msg, create_time FROM task_instance WHERE workflow_instance_id = $1`
	if err := r.db.SelectContext(ctx, &taskDAOs, query, instanceID); err != nil {
		return nil, fmt.Errorf("查询TaskInstance列表失败: %w", err)
	}

	tasks := make([]*storage.TaskInstance, 0, len(taskDAOs))
	for _, taskDAO := range taskDAOs {
		taskInst, _ := r.taskDAOToTaskInstance(&taskDAO)
		tasks = append(tasks, taskInst)
	}

	return tasks, nil
}

// DeleteWorkflowInstance 删除WorkflowInstance及其所有TaskInstance（事务，幂等）
func (r *WorkflowAggregateRepo) DeleteWorkflowInstance(ctx context.Context, instanceID string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = $1`
	if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instanceID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}

	deleteInstSQL := `DELETE FROM workflow_instance WHERE id = $1`
	if _, err := tx.ExecContext(ctx, deleteInstSQL, instanceID); err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// DeleteTaskInstance 删除TaskInstance（幂等）
func (r *WorkflowAggregateRepo) DeleteTaskInstance(ctx context.Context, taskID string) error {
	query := `DELETE FROM task_instance WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, query, taskID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}
	return nil
}

// 确保实现接口
var _ storage.WorkflowAggregateRepository = (*WorkflowAggregateRepo)(nil)
