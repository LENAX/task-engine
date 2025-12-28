package plugin

import "log"

// EmailAlertPlugin 邮件告警插件（对外导出）
type SmsAlertPlugin struct {
	name      string
	url       string
	apiKey    string
	apiSecret string
}

// Name 插件名称（实现Plugin接口，对外导出）
func (e *SmsAlertPlugin) Name() string {
	return e.name
}

// Init 初始化插件（实现Plugin接口，对外导出）
func (e *SmsAlertPlugin) Init(params map[string]string) error {
	e.url = params["url"]
	e.apiKey = params["api_key"]
	e.apiSecret = params["api_secret"]
	log.Println("✅ 短信告警插件初始化完成")
	return nil
}

// Execute 执行邮件告警（实现Plugin接口，对外导出）
func (e *SmsAlertPlugin) Execute(data interface{}) error {
	log.Printf("🔔 发送短信告警：%v", data)
	return nil
}

// NewSmsAlertPlugin 创建短信告警插件（对外导出）
func NewSmsAlertPlugin() Plugin {
	return &SmsAlertPlugin{
		name: "sms_alert",
	}
}
