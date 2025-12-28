package plugin

// Plugin 插件基础接口（对外导出）
type Plugin interface {
	// Name 插件名称（对外导出）
	Name() string
	// Init 初始化插件（对外导出）
	Init(params map[string]string) error
	// Execute 执行插件逻辑（对外导出）
	Execute(data any) error
}
