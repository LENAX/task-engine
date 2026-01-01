package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stevelan1995/task-engine/pkg/storage"
	"github.com/stevelan1995/task-engine/pkg/storage/dao"
)

// taskHandlerRepo SQLite实现（小写，不导出）
type taskHandlerRepo struct {
	db *sqlx.DB
}

// NewTaskHandlerRepo 创建TaskHandler存储实例（内部工厂方法，不导出）
func NewTaskHandlerRepo(db *sqlx.DB) storage.TaskHandlerRepository {
	repo := &taskHandlerRepo{db: db}
	repo.initSchema()
	return repo
}

// initSchema 初始化数据库表结构（内部方法）
func (r *taskHandlerRepo) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS task_handler_meta (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		description TEXT,
		create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_task_handler_meta_name ON task_handler_meta(name);
	`
	_, err := r.db.Exec(createTableSQL)
	return err
}

// Save 实现存储接口（内部实现）
func (r *taskHandlerRepo) Save(ctx context.Context, meta *storage.TaskHandlerMeta) error {
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
	dao := &dao.TaskHandlerDAO{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		CreateTime:  meta.CreateTime,
		UpdateTime:  meta.UpdateTime,
	}

	query := `
	INSERT OR REPLACE INTO task_handler_meta 
	(id, name, description, create_time, update_time)
	VALUES (:id, :name, :description, :create_time, :update_time)
	`
	_, err := r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存TaskHandlerMeta失败: %w", err)
	}
	return nil
}

// GetByName 实现存储接口（内部实现）
func (r *taskHandlerRepo) GetByName(ctx context.Context, name string) (*storage.TaskHandlerMeta, error) {
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

	// 转换为业务实体
	meta := &storage.TaskHandlerMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// GetByID 实现存储接口（内部实现）
func (r *taskHandlerRepo) GetByID(ctx context.Context, id string) (*storage.TaskHandlerMeta, error) {
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

	// 转换为业务实体
	meta := &storage.TaskHandlerMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// ListAll 实现存储接口（内部实现）
func (r *taskHandlerRepo) ListAll(ctx context.Context) ([]*storage.TaskHandlerMeta, error) {
	var daos []dao.TaskHandlerDAO
	query := `SELECT id, name, description, create_time, update_time 
	          FROM task_handler_meta ORDER BY create_time DESC`
	err := r.db.SelectContext(ctx, &daos, query)
	if err != nil {
		return nil, fmt.Errorf("查询所有TaskHandlerMeta失败: %w", err)
	}

	// 转换为业务实体列表
	metas := make([]*storage.TaskHandlerMeta, 0, len(daos))
	for _, d := range daos {
		metas = append(metas, &storage.TaskHandlerMeta{
			ID:          d.ID,
			Name:        d.Name,
			Description: d.Description,
			CreateTime:  d.CreateTime,
			UpdateTime:  d.UpdateTime,
		})
	}

	return metas, nil
}

// Delete 实现存储接口（内部实现，根据ID删除）
// 幂等性：删除不存在的记录不会报错
func (r *taskHandlerRepo) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM task_handler_meta WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("删除TaskHandlerMeta失败: %w", err)
	}
	// 不检查 RowsAffected，删除不存在的记录也是幂等的
	return nil
}

// DeleteByName 根据名称删除Handler元数据（业务接口）
func (r *taskHandlerRepo) DeleteByName(ctx context.Context, name string) error {
	query := `DELETE FROM task_handler_meta WHERE name = ?`
	_, err := r.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("删除TaskHandlerMeta失败: %w", err)
	}
	// 不检查 RowsAffected，删除不存在的记录也是幂等的
	return nil
}
