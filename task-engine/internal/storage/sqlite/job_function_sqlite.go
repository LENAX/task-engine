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

// jobFunctionRepo SQLite实现（小写，不导出）
type jobFunctionRepo struct {
	db *sqlx.DB
}

// NewJobFunctionRepo 创建JobFunction存储实例（内部工厂方法，不导出）
func NewJobFunctionRepo(db *sqlx.DB) storage.JobFunctionRepository {
	repo := &jobFunctionRepo{db: db}
	repo.initSchema()
	return repo
}

// initSchema 初始化数据库表结构（内部方法）
func (r *jobFunctionRepo) initSchema() error {
	createTableSQL := `
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
	_, err := r.db.Exec(createTableSQL)
	return err
}

// Save 实现存储接口（内部实现）
func (r *jobFunctionRepo) Save(ctx context.Context, meta *storage.JobFunctionMeta) error {
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
	dao := &dao.JobFunctionDAO{
		ID:          meta.ID,
		Name:        meta.Name,
		Description: meta.Description,
		CreateTime:  meta.CreateTime,
		UpdateTime:  meta.UpdateTime,
	}

	// 如果JobFunctionMeta有扩展字段（CodePath, Hash, ParamTypes），需要从其他地方获取
	// 这里假设这些字段在后续Phase 6中会添加到JobFunctionMeta结构
	// 暂时使用空值

	query := `
	INSERT OR REPLACE INTO job_function_meta 
	(id, name, description, code_path, hash, param_types, create_time, update_time)
	VALUES (:id, :name, :description, :code_path, :hash, :param_types, :create_time, :update_time)
	`
	_, err := r.db.NamedExecContext(ctx, query, dao)
	if err != nil {
		return fmt.Errorf("保存JobFunctionMeta失败: %w", err)
	}
	return nil
}

// GetByName 实现存储接口（内部实现）
func (r *jobFunctionRepo) GetByName(ctx context.Context, name string) (*storage.JobFunctionMeta, error) {
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

	// 转换为业务实体
	meta := &storage.JobFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// GetByID 实现存储接口（内部实现）
func (r *jobFunctionRepo) GetByID(ctx context.Context, id string) (*storage.JobFunctionMeta, error) {
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

	// 转换为业务实体
	meta := &storage.JobFunctionMeta{
		ID:          dao.ID,
		Name:        dao.Name,
		Description: dao.Description,
		CreateTime:  dao.CreateTime,
		UpdateTime:  dao.UpdateTime,
	}

	return meta, nil
}

// ListAll 实现存储接口（内部实现）
func (r *jobFunctionRepo) ListAll(ctx context.Context) ([]*storage.JobFunctionMeta, error) {
	var daos []dao.JobFunctionDAO
	query := `SELECT id, name, description, code_path, hash, param_types, create_time, update_time 
	          FROM job_function_meta`
	err := r.db.SelectContext(ctx, &daos, query)
	if err != nil {
		return nil, fmt.Errorf("查询JobFunctionMeta列表失败: %w", err)
	}

	result := make([]*storage.JobFunctionMeta, 0, len(daos))
	for _, dao := range daos {
		meta := &storage.JobFunctionMeta{
			ID:          dao.ID,
			Name:        dao.Name,
			Description: dao.Description,
			CreateTime:  dao.CreateTime,
			UpdateTime:  dao.UpdateTime,
		}
		result = append(result, meta)
	}

	return result, nil
}

// Delete 实现存储接口（内部实现）
// 幂等性：删除不存在的记录不会报错
func (r *jobFunctionRepo) Delete(ctx context.Context, name string) error {
	query := `DELETE FROM job_function_meta WHERE name = :name`
	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"name": name,
	})
	if err != nil {
		return fmt.Errorf("删除JobFunctionMeta失败: %w", err)
	}
	// 不检查 RowsAffected，删除不存在的记录也是幂等的
	return nil
}
