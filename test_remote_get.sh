#!/bin/bash
# 测试远程获取脚本

echo "=== 测试远程获取 github.com/LENAX/task-engine ==="
echo ""

# 清理测试目录
cd /tmp && rm -rf test-remote-get && mkdir -p test-remote-get && cd test-remote-get

# 初始化测试项目
echo "1. 初始化测试项目..."
go mod init test-remote

# 尝试获取模块
echo ""
echo "2. 尝试获取模块..."
GOPRIVATE=github.com/LENAX/task-engine go get github.com/LENAX/task-engine@latest 2>&1

# 检查结果
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ 远程获取成功！"
    echo ""
    echo "go.mod 内容："
    cat go.mod
else
    echo ""
    echo "❌ 远程获取失败"
    echo "请确保："
    echo "  1. 代码已推送到 GitHub: git push origin main"
    echo "  2. 已创建新标签: git tag v1.0.1 && git push origin v1.0.1"
fi
