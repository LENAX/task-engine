package storage

// BaseRepository 通用CRUD接口（对外导出）
// 提供基础的增删改查操作，所有Repository都应该实现此接口
// 注意：这是一个标记接口，具体的CRUD接口应该显式定义方法签名
// 所有CRUD接口都应该嵌入此接口，以表明它们提供了基础的CRUD功能
type BaseRepository interface {
	// 此接口仅作为标记，不定义具体方法
	// 具体的CRUD接口应该显式定义 Save、GetByID、Delete 方法
}
