要求：

1. 模拟一个复杂的多步骤工作流，包含多个任务和动态生成的任务，模拟从数据源Tushare批量下载数据的流程
2. 任务包括：
   1. 任务组1（无需依赖，可以马上启动）：
      - 获取tushare交易日历数据（trade_cal）

        - 请求参数：无
        - 返回参数: exchange(str), cal_date(str, yyyymmdd), is_open(str), pre_date(str)
      - 获取tushare股票列表数据 (stock_basic)

        - 请求参数：无
        - 返回参数: ts_code(str), symbol(str), name(str), area(str), industry(str), list_date(str)
   2. 任务组2（依赖任务组1的参数，需等待任务组1都完成后才可以启动，且需要动态生成任务）
      1. 历史日线任务 (daily)
         1. 请求参数：trade_date, 来源于trade_cal.cal_date
            1. 需要从trade_cal的结果动态生成：例如返回['20251201', '20251202,'20251203','20251204','20251205',], 需要先生成5个子任务，加入workflow instance，再等待执行
         2. 返回参数：ts_code(str)，trade_date(str),  open(str),  high(float),   low(float),  close(float),  pre_close(float),  change(float),    pct_chg(float),  vol(int), amount(float)
      2. 复权因子（adj_factor）
         1. 请求参数：ts_code, 来源于stock_basic.ts_code
            1. 需要从stock_basic的结果动态生成：例如返回['000001.SZ', '000002.SZ,'000003.SZ','000004.SZ','000005.SZ',], 需要先生成5个子任务，加入workflow instance，再等待执行
         2. 返回参数：ts_code(str),trade_date(str), adj_factor(float)
   3. 任务函数：
      1. QueryTushare(api_name str, params map[string]any) struct, err
         1. 需要模拟tushare返回数据
         2. 需要校验传入参数是否正确，基本规则是一个ts_code或者一个trade_date对应一行数据。
      2. GenerateTask: 根据依赖任务返回的结果，生成子任务，并将结果填入参数
   4. 任务回调：
      1. 成功：SaveResult, 模拟保存数据
      2. 失败：LogError, 打印错误信息
   5. 依赖：
      1. QuantDataRepository, 由SaveResult调用，模拟插入数据到DuckDB
3. 测试要求：
   1. 正确定义Workflow, 确保正确定义任务和依赖关系
   2. 执行过程需要按依赖关系解锁
   3. 动态任务可以被正确生成并执行
   4. 任务函数可以被正确执行
   5. 任务回调被正确执行
   6. 依赖被获取且可以被执行
   7. 需要打印最后保存的数据，且需要符合预设模拟数据的数量
        1. 按照trade_cal和stock_basic数据的数量，它们返回了5条数据，后面依赖的任务也需要返回5条。注意是每个任务各返回5条。trade_cal和stock_basic会一次性各返回5条记录，然后daily和adj_factor会各自生成5个子任务（需要验证生成子任务的数量），执行完成后，每个任务返回一条，总共10条（daily, adj_factor各5条）

