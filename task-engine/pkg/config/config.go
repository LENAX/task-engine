package config

// Config 引擎核心配置
type Config struct {
    Mode     string `yaml:"mode"`
    HTTPPort int    `yaml:"http_port"`
    Database struct {
        Type     string `yaml:"type"`
        Path     string `yaml:"path"`
        Host     string `yaml:"host"`
        Port     int    `yaml:"port"`
        User     string `yaml:"user"`
        Password string `yaml:"password"`
        DBName   string `yaml:"dbname"`
    } `yaml:"database"`
    Engine struct {
        MaxConcurrency int        `yaml:"max_concurrency"`
        Timeout        string     `yaml:"timeout"`
        Breakpoint     bool       `yaml:"breakpoint"`
    } `yaml:"engine"`
    Plugin struct {
        Builtin struct {
            EmailAlert bool `yaml:"email_alert"`
            SMSAlert   bool `yaml:"sms_alert"`
        } `yaml:"builtin"`
    } `yaml:"plugin"`
}
