package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
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
		status TEXT NOT NULL DEFAULT 'ENABLED'
	);
	`
	_, err := r.db.Exec(createTableSQL)
	return err
}

// Save 实现存储接口（内部实现）
func (r *workflowRepo) Save(ctx context.Context, wf *workflow.Workflow) error {
	// 序列化依赖关系
	depsJSON, err := json.Marshal(wf.GetDependencies())
	if err != nil {
		return fmt.Errorf("序列化依赖关系失败: %w", err)
	}

	// 序列化参数
	paramsJSON, err := json.Marshal(wf.Params)
	if err != nil {
		return fmt.Errorf("序列化参数失败: %w", err)
	}

	// 构建DAO对象
	dao := &dao.WorkflowDAO{
		ID:           wf.GetID(),
		Name:         wf.GetName(),
		Description:  wf.Description,
		Params:       string(paramsJSON),
		Dependencies: string(depsJSON),
		CreateTime:   wf.CreateTime,
		Status:       wf.Status,
	}

	query := `
	INSERT OR REPLACE INTO workflow_definition (id, name, description, params, dependencies, create_time, status)
	VALUES (:id, :name, :description, :params, :dependencies, :create_time, :status)
	`
	_, err = r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存Workflow失败: %w", err)
	}
    return nil
}

// GetByID 实现存储接口（内部实现）
func (r *workflowRepo) GetByID(ctx context.Context, id string) (*workflow.Workflow, error) {
	var dao dao.WorkflowDAO
	query := `SELECT id, name, description, params, dependencies, create_time, status FROM workflow_definition WHERE id = ?`
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
	wf.Params = params
	wf.CreateTime = dao.CreateTime
	wf.Status = dao.Status
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
