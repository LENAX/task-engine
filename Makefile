# 项目基础配置
MODULE := github.com/stevelan1995/quant-task-engine
BINARY_SERVER := bin/server
# 测试相关配置
TEST_DIR := ./test/...          # 所有测试文件目录（符合Go测试规范）
UNIT_TEST_DIR := ./test/unit/... # 单元测试目录
INTEG_TEST_DIR := ./test/integration/... # 集成测试目录
COVER_PROFILE := bin/coverage.out # 覆盖率报告文件
COVER_HTML := bin/coverage.html   # HTML格式覆盖率报告

# 默认目标：显示帮助
.DEFAULT_GOAL := help

# 构建服务端可执行文件
build-server:
	@mkdir -p bin
	go build -o $(BINARY_SERVER) ./cmd/server

# 运行服务端（独立运行main.go）
run-server:
	go run ./cmd/server/main.go

# ===================== 新增测试相关目标 =====================
# 运行所有测试（单元+集成）
test: test-unit test-integration
	@echo "✅ 所有测试执行完成！"

# 仅运行单元测试
test-unit:
	@mkdir -p bin
	go test -v $(UNIT_TEST_DIR) -race # -race 检测数据竞争

# 仅运行集成测试
test-integration:
	@mkdir -p bin
	go test -v $(INTEG_TEST_DIR) -race

# 运行测试并生成覆盖率报告（HTML格式，便于查看）
test-cover:
	@mkdir -p bin
	go test -v $(TEST_DIR) -race -coverprofile=$(COVER_PROFILE) -covermode=atomic
	go tool cover -html=$(COVER_PROFILE) -o $(COVER_HTML)
	@echo "📊 覆盖率报告已生成：$(COVER_HTML)（可直接用浏览器打开）"

# ===================== 原有清理目标 =====================
clean:
	rm -rf bin/
	@echo "🗑️  清理完成！"

# 帮助信息（便捷查看所有命令）
help:
	@echo "📜 可用命令："
	@echo "  make build-server   - 构建服务端可执行文件"
	@echo "  make run-server     - 独立运行main.go（项目主程序）"
	@echo "  make test           - 运行所有测试（单元+集成）"
	@echo "  make test-unit      - 仅运行单元测试"
	@echo "  make test-integration - 仅运行集成测试"
	@echo "  make test-cover     - 运行测试并生成覆盖率报告"
	@echo "  make clean          - 清理构建产物和测试报告"