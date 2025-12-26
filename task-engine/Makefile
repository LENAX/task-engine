# 量化任务引擎Makefile
MODULE := github.com/stevelan1995/task-engine
BINARY_SERVER := bin/server
BINARY_CLI := bin/cli

# 构建服务端
build-server:
	@mkdir -p bin
	go build -o $(BINARY_SERVER) ./cmd/server

# 构建CLI
build-cli:
	@mkdir -p bin
	go build -o $(BINARY_CLI) ./cmd/cli

# 构建所有
build: build-server build-cli

# 运行服务端
run-server:
	go run ./cmd/server

# 运行CLI（示例：make run-cli args="run wf-001"）
run-cli:
	go run ./cmd/cli $(args)

# 单元测试
test-unit:
	go test -v ./test/unit/...

# 集成测试
test-integration:
	go test -v ./test/integration/...

# 清理构建产物
clean:
	rm -rf bin/

.PHONY: build-server build-cli build run-server run-cli test-unit test-integration clean