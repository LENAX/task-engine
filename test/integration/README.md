# 集成测试说明

## Tushare工作流集成测试

### 测试文件
- `tushare_workflow_test.go`: 模拟Tushare数据下载的复杂工作流测试

### 测试场景

#### TestTushareWorkflow_Basic
测试基本的工作流执行流程，包括：
1. **任务组1（无依赖）**：
   - 获取交易日历数据（trade_cal）
   - 获取股票列表数据（stock_basic）

2. **任务回调**：
   - Success Handler: SaveResult（保存数据）
   - Success Handler: GenerateSubTasks（生成子任务，当前仅记录日志）
   - Failed Handler: LogError（记录错误）

3. **依赖注入**：
   - QuantDataRepository（模拟数据仓库）

#### TestTushareWorkflow_WithDependencies
测试包含依赖关系的静态任务执行，包括：
1. 任务组1：获取交易日历、获取股票列表
2. 任务组2（依赖任务组1）：
   - 获取日线数据（依赖交易日历）
   - 获取复权因子（依赖股票列表）

### 已实现功能

✅ **数据打印功能**（需求第7条）：
   - 测试完成后会自动打印所有保存的数据
   - 根据数据类型（trade_cal、stock_basic、daily、adj_factor）格式化输出
   - 包含详细的数据字段信息
   - 显示预期数量和实际数量对比

✅ **数据数量验证**（需求第7条）：
   - 验证保存的数据数量是否符合预设模拟数据的数量
   - 当前静态任务场景：2条（trade_cal + stock_basic）或4条（含静态daily和adj_factor）
   - 完整预期（动态子任务实现后）：12条（1 trade_cal + 1 stock_basic + 5 daily + 5 adj_factor）
   - 验证各类型数据数量是否符合预期

✅ **子任务生成数量验证**（需求第3条）：
   - GenerateSubTasks正确记录需要生成的子任务数量
   - daily子任务：5个（每个交易日1个，不管是否开盘）
   - adj_factor子任务：5个（每只股票1个）
   - 在日志中验证子任务生成数量是否正确

✅ **任务回调执行**（需求第5条）：
   - SaveResult Handler已正确调用并保存数据
   - LogError Handler已实现错误记录
   - GenerateSubTasks Handler已实现（当前记录日志，动态添加功能待完善）

✅ **依赖注入**（需求第6条）：
   - QuantDataRepository通过依赖注入正确获取
   - SaveResult Handler可以成功访问数据仓库并保存数据

✅ **任务函数执行**（需求第4条）：
   - QueryTushare函数正确执行并返回模拟数据
   - 参数校验和基本规则验证已实现
   - trade_cal返回5条交易日数据
   - stock_basic返回5条股票数据

✅ **依赖关系解锁**（需求第2条）：
   - 任务按依赖关系正确执行
   - 任务组1（无依赖）并行执行
   - 任务组2（依赖任务组1）等待前置任务完成后执行

### 预期数据数量说明

根据需求文档，如果动态子任务完全实现，预期数据数量应为：

- **trade_cal**: 1条（汇总数据，包含5个交易日）
- **stock_basic**: 1条（汇总数据，包含5只股票）
- **daily**: 5条（每个交易日1条，共5个交易日）
- **adj_factor**: 5条（每只股票1条，共5只股票）
- **总计**: 12条数据

当前由于动态子任务添加功能尚未完全实现：
- **TestTushareWorkflow_Basic**: 2条（trade_cal + stock_basic）
- **TestTushareWorkflow_WithDependencies**: 4条（trade_cal + stock_basic + 1个daily + 1个adj_factor）

### 已知限制

1. **动态子任务添加**（需求第3条）：
   - `GenerateSubTasks` Handler目前仅记录日志，实际添加子任务到WorkflowInstance的功能待完善
   - 需要Engine提供添加子任务的API
   - 当前测试使用静态任务来验证依赖关系
   - 子任务生成数量验证通过日志记录实现，实际执行待完善

### 运行测试

```bash
# 运行所有集成测试
cd task-engine
go test -v ./test/integration

# 运行特定测试
go test -v ./test/integration -run TestTushareWorkflow_Basic
go test -v ./test/integration -run TestTushareWorkflow_WithDependencies
```

### 测试覆盖

✅ 工作流定义和提交
✅ 任务依赖关系验证
✅ 任务并行执行
✅ 任务回调机制（部分）
✅ 依赖注入机制
✅ 工作流状态管理

### 待完善功能

- [ ] 动态子任务实际添加到WorkflowInstance
- [ ] SaveResult Handler正确调用和数据保存验证
- [ ] 更完整的错误处理测试
- [ ] 工作流暂停/恢复测试
- [ ] 工作流终止测试

