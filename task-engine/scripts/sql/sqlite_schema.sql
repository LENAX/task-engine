-- SQLite数据库表结构
-- 用于异步任务调度引擎

-- Workflow定义表
CREATE TABLE IF NOT EXISTS workflow_definition (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    dependencies TEXT,  -- JSON格式存储依赖关系
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- WorkflowInstance表
CREATE TABLE IF NOT EXISTS workflow_instance (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    status TEXT NOT NULL,  -- Ready/Running/Paused/Terminated/Success/Failed
    start_time DATETIME,
    end_time DATETIME,
    breakpoint TEXT,  -- JSON格式存储断点数据
    error_message TEXT,
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_id) REFERENCES workflow_definition(id)
);

-- Task实例表
CREATE TABLE IF NOT EXISTS task_instance (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    workflow_instance_id TEXT NOT NULL,
    job_func_id TEXT,
    job_func_name TEXT,
    params TEXT,  -- JSON格式存储参数
    status TEXT NOT NULL,  -- Pending/Running/Success/Failed/TimeoutFailed
    timeout_seconds INTEGER DEFAULT 30,
    retry_count INTEGER DEFAULT 0,
    start_time DATETIME,
    end_time DATETIME,
    error_msg TEXT,
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_instance_id) REFERENCES workflow_instance(id)
);

-- Job函数元数据表
CREATE TABLE IF NOT EXISTS job_function_meta (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    code_path TEXT,  -- 函数加载路径
    hash TEXT,  -- 函数二进制哈希
    param_types TEXT,  -- JSON格式存储参数类型列表
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Task Handler元数据表
CREATE TABLE IF NOT EXISTS task_handler_meta (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_workflow_instance_workflow_id ON workflow_instance(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_instance_status ON workflow_instance(status);
CREATE INDEX IF NOT EXISTS idx_task_instance_workflow_instance_id ON task_instance(workflow_instance_id);
CREATE INDEX IF NOT EXISTS idx_task_instance_status ON task_instance(status);
CREATE INDEX IF NOT EXISTS idx_job_function_meta_name ON job_function_meta(name);
CREATE INDEX IF NOT EXISTS idx_task_handler_meta_name ON task_handler_meta(name);

