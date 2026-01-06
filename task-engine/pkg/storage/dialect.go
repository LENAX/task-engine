package storage

// Dialect SQL方言接口（对外导出）
// 封装不同数据库的SQL语法差异
type Dialect interface {
	// Name 返回方言名称（如 "sqlite", "mysql", "postgres"）
	Name() string

	// Placeholder 返回指定位置的占位符
	// SQLite/MySQL: ? (忽略index)
	// PostgreSQL: $1, $2, ...
	Placeholder(index int) string

	// UpsertSQL 返回INSERT或UPDATE的SQL语句
	// tableName: 表名
	// columns: 列名列表
	// conflictColumn: 冲突判断列（通常是主键）
	// updateColumns: 需要更新的列（不含主键）
	UpsertSQL(tableName string, columns []string, conflictColumn string, updateColumns []string) string

	// CreateTableSQL 返回创建表的DDL语句
	// 不同数据库的DDL可能有细微差异
	CreateTableSQL(schema string) string

	// ConfigureDB 配置数据库连接（如SQLite的PRAGMA）
	// 返回需要执行的SQL语句列表
	ConfigureDB() []string

	// AutoIncrementKeyword 返回自增主键关键字
	// SQLite: INTEGER PRIMARY KEY AUTOINCREMENT
	// MySQL: INT PRIMARY KEY AUTO_INCREMENT
	// PostgreSQL: SERIAL PRIMARY KEY
	AutoIncrementKeyword() string

	// BooleanType 返回布尔类型
	// SQLite: INTEGER
	// MySQL: TINYINT(1)
	// PostgreSQL: BOOLEAN
	BooleanType() string

	// TextType 返回文本类型
	// SQLite/PostgreSQL: TEXT
	// MySQL: TEXT 或 LONGTEXT
	TextType() string

	// TimestampType 返回时间戳类型
	// SQLite: DATETIME
	// MySQL: DATETIME
	// PostgreSQL: TIMESTAMP
	TimestampType() string

	// FloatType 返回浮点类型
	// SQLite: REAL
	// MySQL: DOUBLE
	// PostgreSQL: DOUBLE PRECISION
	FloatType() string
}
