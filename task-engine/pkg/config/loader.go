package config

import (
    "os"

    "gopkg.in/yaml.v3"
)

// Load 加载配置文件
func Load(path string) (*Config, error) {
    // 读取配置文件
    data, err := os.ReadFile(path)
    if err != nil {
        // 若文件不存在，返回默认配置
        return &Config{
            Mode:     "dev",
            HTTPPort: 8080,
        }, nil
    }

    // 解析YAML
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}
