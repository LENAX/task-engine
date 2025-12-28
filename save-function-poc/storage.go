package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FunctionMeta 函数元数据
type FunctionMeta struct {
	ID          string    // 函数ID
	Name        string    // 函数名称（唯一）
	Description string    // 函数描述
	CreateTime  time.Time // 创建时间
	// 注意：不再存储ParamTypes和ReturnType，运行时从函数实例通过反射获取
}

// Storage SQLite存储实现
type Storage struct {
	db *sql.DB
}

// NewStorage 创建存储实例
func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	s := &Storage{db: db}
	if err := s.initTables(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close 关闭数据库连接
func (s *Storage) Close() error {
	return s.db.Close()
}

// initTables 初始化数据库表
func (s *Storage) initTables() error {
	// 创建函数元数据表（简化版：只保存ID和名称）
	createFuncTable := `
	CREATE TABLE IF NOT EXISTS functions (
		id TEXT PRIMARY KEY,
		name TEXT UNIQUE NOT NULL,
		description TEXT,
		create_time DATETIME NOT NULL
	);`

	// 创建任务表
	createTaskTable := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		func_id TEXT NOT NULL,
		params TEXT,
		create_time DATETIME NOT NULL,
		FOREIGN KEY(func_id) REFERENCES functions(id)
	);`

	if _, err := s.db.Exec(createFuncTable); err != nil {
		return err
	}
	if _, err := s.db.Exec(createTaskTable); err != nil {
		return err
	}
	return nil
}

// SaveFunction 保存函数元数据
func (s *Storage) SaveFunction(ctx context.Context, meta *FunctionMeta) error {
	query := `
	INSERT OR REPLACE INTO functions (id, name, description, create_time)
	VALUES (?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		meta.ID,
		meta.Name,
		meta.Description,
		meta.CreateTime.Format(time.RFC3339),
	)
	return err
}

// GetFunctionByID 根据ID获取函数元数据
func (s *Storage) GetFunctionByID(ctx context.Context, id string) (*FunctionMeta, error) {
	query := `SELECT id, name, description, create_time FROM functions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	meta := &FunctionMeta{}
	var createTimeStr string

	err := row.Scan(
		&meta.ID,
		&meta.Name,
		&meta.Description,
		&createTimeStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	// 解析时间
	if meta.CreateTime, err = time.Parse(time.RFC3339, createTimeStr); err != nil {
		// 如果解析失败，尝试其他格式
		if meta.CreateTime, err = time.Parse("2006-01-02 15:04:05", createTimeStr); err != nil {
			meta.CreateTime, _ = time.Parse("2006-01-02T15:04:05Z07:00", createTimeStr)
		}
	}

	return meta, nil
}

// GetFunctionByName 根据名称获取函数元数据
func (s *Storage) GetFunctionByName(ctx context.Context, name string) (*FunctionMeta, error) {
	query := `SELECT id, name, description, create_time FROM functions WHERE name = ?`
	row := s.db.QueryRowContext(ctx, query, name)

	meta := &FunctionMeta{}
	var createTimeStr string

	err := row.Scan(
		&meta.ID,
		&meta.Name,
		&meta.Description,
		&createTimeStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	// 解析时间
	if meta.CreateTime, err = time.Parse(time.RFC3339, createTimeStr); err != nil {
		// 如果解析失败，尝试其他格式
		if meta.CreateTime, err = time.Parse("2006-01-02 15:04:05", createTimeStr); err != nil {
			meta.CreateTime, _ = time.Parse("2006-01-02T15:04:05Z07:00", createTimeStr)
		}
	}

	return meta, nil
}

// ListAllFunctions 列出所有函数
func (s *Storage) ListAllFunctions(ctx context.Context) ([]*FunctionMeta, error) {
	query := `SELECT id, name, description, create_time FROM functions`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var functions []*FunctionMeta
	for rows.Next() {
		meta := &FunctionMeta{}
		var createTimeStr string

		err := rows.Scan(
			&meta.ID,
			&meta.Name,
			&meta.Description,
			&createTimeStr,
		)
		if err != nil {
			return nil, err
		}

		// 解析时间
		if meta.CreateTime, err = time.Parse(time.RFC3339, createTimeStr); err != nil {
			// 如果解析失败，尝试其他格式
			if meta.CreateTime, err = time.Parse("2006-01-02 15:04:05", createTimeStr); err != nil {
				meta.CreateTime, _ = time.Parse("2006-01-02T15:04:05Z07:00", createTimeStr)
			}
		}

		functions = append(functions, meta)
	}

	return functions, rows.Err()
}

// SaveTask 保存任务
func (s *Storage) SaveTask(ctx context.Context, task *Task) error {
	paramsJSON, err := json.Marshal(task.Params)
	if err != nil {
		return err
	}

	query := `
	INSERT OR REPLACE INTO tasks (id, name, description, func_id, params, create_time)
	VALUES (?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		task.ID,
		task.Name,
		task.Description,
		task.FuncID,
		string(paramsJSON),
		task.CreateTime.Format(time.RFC3339),
	)
	return err
}

// GetTaskByID 根据ID获取任务
func (s *Storage) GetTaskByID(ctx context.Context, id string) (*Task, error) {
	query := `SELECT id, name, description, func_id, params, create_time FROM tasks WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	task := &Task{}
	var paramsJSON string
	var createTimeStr string

	err := row.Scan(
		&task.ID,
		&task.Name,
		&task.Description,
		&task.FuncID,
		&paramsJSON,
		&createTimeStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	// 解析参数
	if err := json.Unmarshal([]byte(paramsJSON), &task.Params); err != nil {
		return nil, err
	}

	// 解析时间
	if task.CreateTime, err = time.Parse(time.RFC3339, createTimeStr); err != nil {
		// 如果解析失败，尝试其他格式
		if task.CreateTime, err = time.Parse("2006-01-02 15:04:05", createTimeStr); err != nil {
			task.CreateTime, _ = time.Parse("2006-01-02T15:04:05Z07:00", createTimeStr)
		}
	}

	return task, nil
}

// ListAllTasks 列出所有任务
func (s *Storage) ListAllTasks(ctx context.Context) ([]*Task, error) {
	query := `SELECT id, name, description, func_id, params, create_time FROM tasks`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task := &Task{}
		var paramsJSON string
		var createTimeStr string

		err := rows.Scan(
			&task.ID,
			&task.Name,
			&task.Description,
			&task.FuncID,
			&paramsJSON,
			&createTimeStr,
		)
		if err != nil {
			return nil, err
		}

		// 解析参数
		if err := json.Unmarshal([]byte(paramsJSON), &task.Params); err != nil {
			return nil, err
		}

		// 解析时间
		if task.CreateTime, err = time.Parse(time.RFC3339, createTimeStr); err != nil {
			task.CreateTime, _ = time.Parse("2006-01-02 15:04:05", createTimeStr)
		}

		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}
