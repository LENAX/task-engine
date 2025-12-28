package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/storage"
	"github.com/stevelan1995/task-engine/pkg/storage/dao"
)

// taskRepo SQLite实现（小写，不导出）
type taskRepo struct {
	db *sqlx.DB
}

// NewTaskRepo 创建Task存储实例（内部工厂方法，不导出）
func NewTaskRepo(db *sqlx.DB) storage.TaskRepository {
	repo := &taskRepo{db: db}
	repo.initSchema()
	return repo
}

// initSchema 初始化数据库表结构（内部方法）
func (r *taskRepo) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS task_instance (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		workflow_instance_id TEXT NOT NULL,
		job_func_id TEXT,
		job_func_name TEXT,
		params TEXT,
		status TEXT NOT NULL,
		timeout_seconds INTEGER DEFAULT 30,
		retry_count INTEGER DEFAULT 0,
		start_time DATETIME,
		end_time DATETIME,
		error_msg TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_task_instance_workflow_instance_id ON task_instance(workflow_instance_id);
	CREATE INDEX IF NOT EXISTS idx_task_instance_status ON task_instance(status);
	`
	_, err := r.db.Exec(createTableSQL)
	return err
}

// Save 保存Task实例
func (r *taskRepo) Save(ctx context.Context, task *storage.TaskInstance) error {
	paramsJSON, err := json.Marshal(task.Params)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	// 构建DAO对象
	dao := &dao.TaskDAO{
		ID:                 task.ID,
		Name:               task.Name,
		WorkflowInstanceID: task.WorkflowInstanceID,
		JobFuncName:        task.JobFuncName,
		Params:             string(paramsJSON),
		Status:             task.Status,
		TimeoutSeconds:     task.TimeoutSeconds,
		RetryCount:         task.RetryCount,
		CreateTime:         task.CreateTime,
	}

	if task.JobFuncID != "" {
		dao.JobFuncID.Valid = true
		dao.JobFuncID.String = task.JobFuncID
	}
	if task.StartTime != nil {
		dao.StartTime.Valid = true
		dao.StartTime.Time = *task.StartTime
	}
	if task.EndTime != nil {
		dao.EndTime.Valid = true
		dao.EndTime.Time = *task.EndTime
	}
	if task.ErrorMessage != "" {
		dao.ErrorMessage.Valid = true
		dao.ErrorMessage.String = task.ErrorMessage
	}

	query := `
	INSERT OR REPLACE INTO task_instance 
	(id, name, workflow_instance_id, job_func_id, job_func_name, params, status, 
	 timeout_seconds, retry_count, start_time, end_time, error_msg, create_time)
	VALUES (:id, :name, :workflow_instance_id, :job_func_id, :job_func_name, :params, :status, 
	 :timeout_seconds, :retry_count, :start_time, :end_time, :error_msg, :create_time)
	`
	_, err = r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存Task失败: %w", err)
	}
	return nil
}

// GetByID 根据ID查询Task实例
func (r *taskRepo) GetByID(ctx context.Context, id string) (*storage.TaskInstance, error) {
	var dao dao.TaskDAO
	query := `
	SELECT id, name, workflow_instance_id, job_func_id, job_func_name, params, status,
	       timeout_seconds, retry_count, start_time, end_time, error_msg, create_time
	FROM task_instance WHERE id = ?
	`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询Task失败: %w", err)
	}

	// 转换为业务实体
	task := &storage.TaskInstance{
		ID:                 dao.ID,
		Name:               dao.Name,
		WorkflowInstanceID: dao.WorkflowInstanceID,
		JobFuncName:        dao.JobFuncName,
		Status:             dao.Status,
		TimeoutSeconds:     dao.TimeoutSeconds,
		RetryCount:         dao.RetryCount,
		CreateTime:         dao.CreateTime,
	}

	if dao.JobFuncID.Valid {
		task.JobFuncID = dao.JobFuncID.String
	}
	if dao.StartTime.Valid {
		task.StartTime = &dao.StartTime.Time
	}
	if dao.EndTime.Valid {
		task.EndTime = &dao.EndTime.Time
	}
	if dao.ErrorMessage.Valid {
		task.ErrorMessage = dao.ErrorMessage.String
	}

	// 反序列化参数
	if dao.Params != "" {
		if err := json.Unmarshal([]byte(dao.Params), &task.Params); err != nil {
			return nil, fmt.Errorf("反序列化参数失败: %w", err)
		}
	} else {
		task.Params = make(map[string]interface{})
	}

	return task, nil
}

// GetByWorkflowInstanceID 根据WorkflowInstance ID查询所有Task
func (r *taskRepo) GetByWorkflowInstanceID(ctx context.Context, instanceID string) ([]*storage.TaskInstance, error) {
	var daos []dao.TaskDAO
	query := `
	SELECT id, name, workflow_instance_id, job_func_id, job_func_name, params, status,
	       timeout_seconds, retry_count, start_time, end_time, error_msg, create_time
	FROM task_instance WHERE workflow_instance_id = ?
	`
	err := r.db.SelectContext(ctx, &daos, query, instanceID)
	if err != nil {
		return nil, fmt.Errorf("查询Task列表失败: %w", err)
	}

	tasks := make([]*storage.TaskInstance, 0, len(daos))
	for _, dao := range daos {
		task := &storage.TaskInstance{
			ID:                 dao.ID,
			Name:               dao.Name,
			WorkflowInstanceID: dao.WorkflowInstanceID,
			JobFuncName:        dao.JobFuncName,
			Status:             dao.Status,
			TimeoutSeconds:     dao.TimeoutSeconds,
			RetryCount:         dao.RetryCount,
			CreateTime:         dao.CreateTime,
		}

		if dao.JobFuncID.Valid {
			task.JobFuncID = dao.JobFuncID.String
		}
		if dao.StartTime.Valid {
			task.StartTime = &dao.StartTime.Time
		}
		if dao.EndTime.Valid {
			task.EndTime = &dao.EndTime.Time
		}
		if dao.ErrorMessage.Valid {
			task.ErrorMessage = dao.ErrorMessage.String
		}

		// 反序列化参数
		if dao.Params != "" {
			if err := json.Unmarshal([]byte(dao.Params), &task.Params); err != nil {
				return nil, fmt.Errorf("反序列化参数失败: %w", err)
			}
		} else {
			task.Params = make(map[string]interface{})
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// UpdateStatus 更新Task状态
func (r *taskRepo) UpdateStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE task_instance SET status = :status WHERE id = :id`
	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"status": status,
		"id":     id,
	})
	if err != nil {
		return fmt.Errorf("更新Task状态失败: %w", err)
	}
	return nil
}

// UpdateStatusWithError 更新Task状态和错误信息
func (r *taskRepo) UpdateStatusWithError(ctx context.Context, id string, status string, errorMsg string) error {
	query := `UPDATE task_instance SET status = :status, error_msg = :error_msg WHERE id = :id`
	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"status":    status,
		"error_msg": errorMsg,
		"id":        id,
	})
	if err != nil {
		return fmt.Errorf("更新Task状态和错误信息失败: %w", err)
	}
	return nil
}

// Delete 删除Task实例
func (r *taskRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM task_instance WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("删除Task失败: %w", err)
	}
	return nil
}
