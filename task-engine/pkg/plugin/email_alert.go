package plugin

import "log"

// EmailAlertPlugin 邮件告警插件（对外导出）
type EmailAlertPlugin struct {
    name string
    smtpHost string
    smtpPort int
}

// Name 插件名称（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Name() string {
    return e.name
}

// Init 初始化插件（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Init(params map[string]string) error {
    e.smtpHost = params["smtp_host"]
    e.smtpPort = 25
    log.Println("✅ 邮件告警插件初始化完成")
    return nil
}

// Execute 执行邮件告警（实现Plugin接口，对外导出）
func (e *EmailAlertPlugin) Execute(data interface{}) error {
    log.Printf("📧 发送邮件告警：%v", data)
    return nil
}
