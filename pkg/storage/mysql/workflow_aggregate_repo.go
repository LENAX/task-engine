package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LENAX/task-engine/pkg/core/task"
	"github.com/LENAX/task-engine/pkg/core/workflow"
	"github.com/LENAX/task-engine/pkg/storage"
	"github.com/LENAX/task-engine/pkg/storage/dao"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// WorkflowAggregateRepo Workflow聚合根Repository的MySQL实现（对外导出）
type WorkflowAggregateRepo struct {
	db      *sqlx.DB
	dialect *MySQLDialect
}

// NewWorkflowAggregateRepo 创建MySQL Workflow聚合根Repository实例（对外导出）
func NewWorkflowAggregateRepo(db *sqlx.DB) (*WorkflowAggregateRepo, error) {
	repo := &WorkflowAggregateRepo{
		db:      db,
		dialect: NewMySQLDialect(),
	}
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	return repo, nil
}

// NewWorkflowAggregateRepoFromDSN 通过DSN创建MySQL Workflow聚合根Repository实例（对外导出）
// dsn格式: user:password@tcp(host:port)/dbname?parseTime=true
func NewWorkflowAggregateRepoFromDSN(dsn string) (*WorkflowAggregateRepo, error) {
	// 确保DSN包含parseTime=true
	if !strings.Contains(dsn, "parseTime=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&parseTime=true"
		} else {
			dsn += "?parseTime=true"
		}
	}

	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	// 配置MySQL
	dialect := NewMySQLDialect()
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
		name VARCHAR(255) NOT NULL UNIQUE,
		description TEXT,
		params TEXT,
		dependencies TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status VARCHAR(50) NOT NULL DEFAULT 'ENABLED',
		sub_task_error_tolerance DOUBLE NOT NULL DEFAULT 0.0,
		transactional TINYINT(1) NOT NULL DEFAULT 0,
		transaction_mode VARCHAR(50) DEFAULT '',
		max_concurrent_task INT NOT NULL DEFAULT 10,
		cron_expr VARCHAR(100) DEFAULT '',
		cron_enabled TINYINT(1) NOT NULL DEFAULT 0
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	// Task定义表
	createTaskDefSQL := `
	CREATE TABLE IF NOT EXISTS task_definition (
		id VARCHAR(36) PRIMARY KEY,
		workflow_id VARCHAR(36) NOT NULL,
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
		is_template TINYINT(1) DEFAULT 0,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_id) REFERENCES workflow_definition(id) ON DELETE CASCADE,
		UNIQUE KEY uk_task_definition_workflow_name (workflow_id, name),
		INDEX idx_task_definition_workflow_id (workflow_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	// WorkflowInstance表
	createInstanceSQL := `
	CREATE TABLE IF NOT EXISTS workflow_instance (
		id VARCHAR(36) PRIMARY KEY,
		workflow_id VARCHAR(36) NOT NULL,
		status VARCHAR(50) NOT NULL,
		start_time DATETIME,
		end_time DATETIME,
		breakpoint TEXT,
		error_message TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_id) REFERENCES workflow_definition(id),
		INDEX idx_workflow_instance_workflow_id (workflow_id),
		INDEX idx_workflow_instance_status (status)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	// TaskInstance表
	createTaskInstSQL := `
	CREATE TABLE IF NOT EXISTS task_instance (
		id VARCHAR(36) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		workflow_instance_id VARCHAR(36) NOT NULL,
		job_func_id VARCHAR(36),
		job_func_name VARCHAR(255),
		compensation_func_id VARCHAR(36),
		compensation_func_name VARCHAR(255),
		params TEXT,
		status VARCHAR(50) NOT NULL,
		timeout_seconds INT DEFAULT 30,
		retry_count INT DEFAULT 0,
		start_time DATETIME,
		end_time DATETIME,
		error_msg TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (workflow_instance_id) REFERENCES workflow_instance(id) ON DELETE CASCADE,
		INDEX idx_task_instance_workflow_instance_id (workflow_instance_id),
		INDEX idx_task_instance_status (status)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`

	for _, sql := range []string{createWorkflowSQL, createTaskDefSQL, createInstanceSQL, createTaskInstSQL} {
		if _, err := r.db.Exec(sql); err != nil {
			// MySQL会报错如果表已存在索引等，忽略这些错误
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

	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = ?`
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
	workflowID := wf.GetID()
	if existingID != "" {
		workflowID = existingID
	}

	workflowDAO := &dao.WorkflowDAO{
		ID:                    workflowID,
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

	query := `
	INSERT INTO workflow_definition 
	(id, name, description, params, dependencies, create_time, status, sub_task_error_tolerance, 
	 transactional, transaction_mode, max_concurrent_task, cron_expr, cron_enabled)
	VALUES (:id, :name, :description, :params, :dependencies, :create_time, :status, :sub_task_error_tolerance,
	 :transactional, :transaction_mode, :max_concurrent_task, :cron_expr, :cron_enabled)
	ON DUPLICATE KEY UPDATE
	 name = VALUES(name), description = VALUES(description), params = VALUES(params),
	 dependencies = VALUES(dependencies), status = VALUES(status),
	 sub_task_error_tolerance = VALUES(sub_task_error_tolerance),
	 transactional = VALUES(transactional), transaction_mode = VALUES(transaction_mode),
	 max_concurrent_task = VALUES(max_concurrent_task), cron_expr = VALUES(cron_expr),
	 cron_enabled = VALUES(cron_enabled)
	`
	if _, err := tx.NamedExecContext(ctx, query, workflowDAO); err != nil {
		// 如果插入失败且是name唯一性约束冲突（并发情况下可能发生），重新查询并更新
		if errStr := err.Error(); errStr != "" && strings.Contains(errStr, "Duplicate entry") && strings.Contains(errStr, "for key 'name'") {
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

	// 先检查是否存在相同(workflow_id, name)或id的task（幂等性：根据id或(workflow_id, name)排除重复）
	var existingID string
	checkQuery := `SELECT id FROM task_definition WHERE (workflow_id = ? AND name = ?) OR id = ? LIMIT 1`
	if err := tx.GetContext(ctx, &existingID, checkQuery, workflowID, taskObj.GetName(), taskObj.GetID()); err != nil {
		// 如果不存在，existingID保持为空字符串，将使用新的id插入
		if err.Error() != "sql: no rows in result set" {
			return fmt.Errorf("检查Task定义是否存在失败: %w", err)
		}
	}

	// 如果找到已存在的记录，使用其id进行更新（幂等性）
	taskID := taskObj.GetID()
	if existingID != "" {
		taskID = existingID
	}

	taskDefDAO := &dao.TaskDefinitionDAO{
		ID:                   taskID,
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

	query := `
	INSERT INTO task_definition
	(id, workflow_id, name, description, job_func_id, job_func_name, compensation_func_id, compensation_func_name,
	 params, timeout_seconds, retry_count, dependencies, required_params, result_mapping, status_handlers, is_template, create_time)
	VALUES (:id, :workflow_id, :name, :description, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :timeout_seconds, :retry_count, :dependencies, :required_params, :result_mapping, :status_handlers, :is_template, :create_time)
	ON DUPLICATE KEY UPDATE
	 workflow_id = VALUES(workflow_id), name = VALUES(name), description = VALUES(description), job_func_id = VALUES(job_func_id),
	 job_func_name = VALUES(job_func_name), compensation_func_id = VALUES(compensation_func_id),
	 compensation_func_name = VALUES(compensation_func_name), params = VALUES(params),
	 timeout_seconds = VALUES(timeout_seconds), retry_count = VALUES(retry_count),
	 dependencies = VALUES(dependencies), required_params = VALUES(required_params),
	 result_mapping = VALUES(result_mapping), status_handlers = VALUES(status_handlers),
	 is_template = VALUES(is_template)
	`
	if _, err := tx.NamedExecContext(ctx, query, taskDefDAO); err != nil {
		// 如果插入失败且是(workflow_id, name)唯一性约束冲突（并发情况下可能发生），重新查询并更新
		if errStr := err.Error(); errStr != "" && strings.Contains(errStr, "Duplicate entry") && strings.Contains(errStr, "for key 'uk_task_definition_workflow_name'") {
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
	query := `SELECT id FROM workflow_instance WHERE workflow_id = ?`
	if err := tx.SelectContext(ctx, &instanceIDs, query, id); err != nil {
		return fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	for _, instID := range instanceIDs {
		deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = ?`
		if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instID); err != nil {
			return fmt.Errorf("删除TaskInstance失败: %w", err)
		}
	}

	deleteInstSQL := `DELETE FROM workflow_instance WHERE workflow_id = ?`
	if _, err := tx.ExecContext(ctx, deleteInstSQL, id); err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}

	deleteTaskDefSQL := `DELETE FROM task_definition WHERE workflow_id = ?`
	if _, err := tx.ExecContext(ctx, deleteTaskDefSQL, id); err != nil {
		return fmt.Errorf("删除Task定义失败: %w", err)
	}

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
	INSERT INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, create_time)
	VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :status, :timeout_seconds, :retry_count, :create_time)
	ON DUPLICATE KEY UPDATE
	 name = VALUES(name), job_func_id = VALUES(job_func_id), job_func_name = VALUES(job_func_name),
	 compensation_func_id = VALUES(compensation_func_id), compensation_func_name = VALUES(compensation_func_name),
	 params = VALUES(params), status = VALUES(status), timeout_seconds = VALUES(timeout_seconds),
	 retry_count = VALUES(retry_count)
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
	          error_msg, create_time FROM task_instance WHERE id = ?`
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

// SaveTaskInstance 保存TaskInstance
func (r *WorkflowAggregateRepo) SaveTaskInstance(ctx context.Context, taskInst *storage.TaskInstance) error {
	paramsJSON, _ := json.Marshal(taskInst.Params)

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
	INSERT INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, compensation_func_id, compensation_func_name, 
	 params, status, timeout_seconds, retry_count, start_time, end_time, error_msg, create_time)
	VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :compensation_func_id, :compensation_func_name,
	 :params, :status, :timeout_seconds, :retry_count, :start_time, :end_time, :error_msg, :create_time)
	ON DUPLICATE KEY UPDATE
	 name = VALUES(name), job_func_id = VALUES(job_func_id), job_func_name = VALUES(job_func_name),
	 compensation_func_id = VALUES(compensation_func_id), compensation_func_name = VALUES(compensation_func_name),
	 params = VALUES(params), status = VALUES(status), timeout_seconds = VALUES(timeout_seconds),
	 retry_count = VALUES(retry_count), start_time = VALUES(start_time), end_time = VALUES(end_time),
	 error_msg = VALUES(error_msg)
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

	deleteTaskInstSQL := `DELETE FROM task_instance WHERE workflow_instance_id = ?`
	if _, err := tx.ExecContext(ctx, deleteTaskInstSQL, instanceID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}

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
func (r *WorkflowAggregateRepo) DeleteTaskInstance(ctx context.Context, taskID string) error {
	query := `DELETE FROM task_instance WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, taskID); err != nil {
		return fmt.Errorf("删除TaskInstance失败: %w", err)
	}
	return nil
}

// 确保实现接口
var _ storage.WorkflowAggregateRepository = (*WorkflowAggregateRepo)(nil)
