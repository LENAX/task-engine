package plugin

// Plugin 插件基础接口（对外导出）
type Plugin interface {
    // Name 插件名称（对外导出）
    Name() string
    // Init 初始化插件（对外导出）
    Init(params map[string]string) error
    // Execute 执行插件逻辑（对外导出）
    Execute(data interface{}) error
}

// NewEmailAlertPlugin 创建邮件告警插件（对外导出）
func NewEmailAlertPlugin() Plugin {
    return &EmailAlertPlugin{
        name: "email_alert",
    }
}

// NewSmsAlertPlugin 创建短信告警插件（对外导出）
func NewSmsAlertPlugin() Plugin {
    return &SmsAlertPlugin{
        name: "sms_alert",
    }
}
