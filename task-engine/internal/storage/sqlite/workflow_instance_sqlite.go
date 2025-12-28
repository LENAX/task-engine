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

// workflowInstanceRepo SQLite实现（小写，不导出）
type workflowInstanceRepo struct {
	db *sqlx.DB
}

// NewWorkflowInstanceRepo 创建WorkflowInstance存储实例（内部工厂方法，不导出）
func NewWorkflowInstanceRepo(db *sqlx.DB) storage.WorkflowInstanceRepository {
	repo := &workflowInstanceRepo{db: db}
	repo.initSchema()
	return repo
}

// initSchema 初始化数据库表结构（内部方法）
func (r *workflowInstanceRepo) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS workflow_instance (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		status TEXT NOT NULL,
		start_time DATETIME,
		end_time DATETIME,
		breakpoint TEXT,
		error_message TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_workflow_id ON workflow_instance(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_instance_status ON workflow_instance(status);
	`
	_, err := r.db.Exec(createTableSQL)
	return err
}

// Save 保存WorkflowInstance
func (r *workflowInstanceRepo) Save(ctx context.Context, instance *storage.WorkflowInstance) error {
	// 序列化断点数据
	var breakpointJSON string
	if instance.Breakpoint != nil {
		jsonBytes, err := json.Marshal(instance.Breakpoint)
		if err != nil {
			return fmt.Errorf("序列化断点数据失败: %w", err)
		}
		breakpointJSON = string(jsonBytes)
	}

	// 构建DAO对象
	dao := &dao.WorkflowInstanceDAO{
		ID:         instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     instance.Status,
		CreateTime: instance.CreateTime,
	}

	if !instance.StartTime.IsZero() {
		dao.StartTime.Valid = true
		dao.StartTime.Time = instance.StartTime
	}
	if instance.EndTime != nil {
		dao.EndTime.Valid = true
		dao.EndTime.Time = *instance.EndTime
	}
	if breakpointJSON != "" {
		dao.Breakpoint.Valid = true
		dao.Breakpoint.String = breakpointJSON
	}
	if instance.ErrorMessage != "" {
		dao.ErrorMessage.Valid = true
		dao.ErrorMessage.String = instance.ErrorMessage
	}

	query := `
	INSERT OR REPLACE INTO workflow_instance 
	(id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time)
	VALUES (:id, :workflow_id, :status, :start_time, :end_time, :breakpoint, :error_message, :create_time)
	`
	_, err := r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存WorkflowInstance失败: %w", err)
	}
	return nil
}

// GetByID 根据ID查询WorkflowInstance
func (r *workflowInstanceRepo) GetByID(ctx context.Context, id string) (*storage.WorkflowInstance, error) {
	var dao dao.WorkflowInstanceDAO
	query := `
	SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	FROM workflow_instance WHERE id = ?
	`
	err := r.db.GetContext(ctx, &dao, query, id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("查询WorkflowInstance失败: %w", err)
	}

	// 转换为业务实体
	instance := &storage.WorkflowInstance{
		ID:         dao.ID,
		WorkflowID: dao.WorkflowID,
		Status:     dao.Status,
		CreateTime: dao.CreateTime,
	}

	if dao.StartTime.Valid {
		instance.StartTime = dao.StartTime.Time
	}
	if dao.EndTime.Valid {
		instance.EndTime = &dao.EndTime.Time
	}
	if dao.Breakpoint.Valid && dao.Breakpoint.String != "" {
		var breakpoint storage.BreakpointData
		if err := json.Unmarshal([]byte(dao.Breakpoint.String), &breakpoint); err != nil {
			return nil, fmt.Errorf("反序列化断点数据失败: %w", err)
		}
		instance.Breakpoint = &breakpoint
	}
	if dao.ErrorMessage.Valid {
		instance.ErrorMessage = dao.ErrorMessage.String
	}

	return instance, nil
}

// UpdateStatus 更新WorkflowInstance状态
func (r *workflowInstanceRepo) UpdateStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE workflow_instance SET status = :status WHERE id = :id`
	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"status": status,
		"id":     id,
	})
	if err != nil {
		return fmt.Errorf("更新WorkflowInstance状态失败: %w", err)
	}
	return nil
}

// UpdateBreakpoint 更新断点数据
func (r *workflowInstanceRepo) UpdateBreakpoint(ctx context.Context, id string, breakpoint *storage.BreakpointData) error {
	var breakpointJSON string
	if breakpoint != nil {
		jsonBytes, err := json.Marshal(breakpoint)
		if err != nil {
			return fmt.Errorf("序列化断点数据失败: %w", err)
		}
		breakpointJSON = string(jsonBytes)
	}

	query := `UPDATE workflow_instance SET breakpoint = :breakpoint WHERE id = :id`
	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"breakpoint": breakpointJSON,
		"id":         id,
	})
	if err != nil {
		return fmt.Errorf("更新断点数据失败: %w", err)
	}
	return nil
}

// ListByStatus 根据状态查询WorkflowInstance列表
func (r *workflowInstanceRepo) ListByStatus(ctx context.Context, status string) ([]*storage.WorkflowInstance, error) {
	var daos []dao.WorkflowInstanceDAO
	query := `
	SELECT id, workflow_id, status, start_time, end_time, breakpoint, error_message, create_time
	FROM workflow_instance WHERE status = ?
	`
	err := r.db.SelectContext(ctx, &daos, query, status)
	if err != nil {
		return nil, fmt.Errorf("查询WorkflowInstance列表失败: %w", err)
	}

	instances := make([]*storage.WorkflowInstance, 0, len(daos))
	for _, dao := range daos {
		instance := &storage.WorkflowInstance{
			ID:         dao.ID,
			WorkflowID: dao.WorkflowID,
			Status:     dao.Status,
			CreateTime: dao.CreateTime,
		}

		if dao.StartTime.Valid {
			instance.StartTime = dao.StartTime.Time
		}
		if dao.EndTime.Valid {
			instance.EndTime = &dao.EndTime.Time
		}
		if dao.Breakpoint.Valid && dao.Breakpoint.String != "" {
			var breakpoint storage.BreakpointData
			if err := json.Unmarshal([]byte(dao.Breakpoint.String), &breakpoint); err != nil {
				return nil, fmt.Errorf("反序列化断点数据失败: %w", err)
			}
			instance.Breakpoint = &breakpoint
		}
		if dao.ErrorMessage.Valid {
			instance.ErrorMessage = dao.ErrorMessage.String
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

// Delete 删除WorkflowInstance
func (r *workflowInstanceRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM workflow_instance WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("删除WorkflowInstance失败: %w", err)
	}
	return nil
}
