我们需要给任务引擎补充一些特性来提高可靠性：

1. 依赖参数校验：

   1. 下游任务声明的参数需要检查上游任务是否提供。
   2. 上游任务的结果可能跟下游任务依赖的参数字段名不一样，需要补充映射规则。可以在任务声明处补充一个字段 resultMapping
2. 支持动态参数生成和注入
3. 暂时不需要限制子任务生成
4. 支持多handler按顺序执行，并对外导出一些默认的handler

   默认 Handler 列表与功能描述：

   ## 默认 Handler 列表

   | Handler 名称                   | 功能描述                               | 适用状态          | 优先级 | 配置参数                          |
   | ------------------------------ | -------------------------------------- | ----------------- | ------ | --------------------------------- |
   | DefaultLogSuccess              | 记录任务成功执行的日志                 | Success           | 高     | 无                                |
   | DefaultLogError                | 记录任务失败的日志                     | Failed            | 高     | 无                                |
   | DefaultSaveResult              | 将任务结果保存到Repository             | Success           | 高     | 无（需注入DataRepository依赖）    |
   | DefaultAggregateSubTaskResults | 聚合子任务结果，计算成功率、总数据量等 | Success           | 高     | success_rate_threshold (默认80.0) |
   | DefaultBatchGenerateSubTasks   | 批量生成子任务，限制每批数量           | Success           | 高     | batch_size (默认10)               |
   | DefaultValidateParams          | 校验任务参数的完整性和合法性           | Start/Success     | 中     | required_params, param_validators |
   | DefaultCompensate              | 执行补偿逻辑，回滚已执行的子任务       | Failed/Compensate | 中     | compensate_actions                |
   | DefaultSkipIfCached            | 缓存命中时跳过任务执行                 | Start             | 中     | cache_key, cache_ttl              |
   | DefaultRetryOnFailure          | 失败时自动重试（增强版）               | Failed            | 低     | max_retries, retry_delay          |
   | DefaultNotifyOnFailure         | 失败时发送通知（邮件/短信等）          | Failed            | 低     | notification_channels             |

   ## 详细功能说明

   ### 1. 基础日志 Handler

   DefaultLogSuccess* 功能：记录任务成功执行的日志


   * 输出：任务ID、任务名称、执行结果
   * 使用场景：所有需要记录成功日志的任务

   DefaultLogError* 功能：记录任务失败的日志

   * 输出：任务ID、任务名称、错误信息
   * 使用场景：所有需要记录错误日志的任务

   ### 2. 数据持久化 Handler

   DefaultSaveResult* 功能：将任务结果保存到依赖注入的Repository

   * 依赖：需要提供Repository/DAO模块依赖的名称和类型，实现 Save(data map[string]interface{}) error 方法
   * 保存内容：任务ID、任务名称、结果数据、时间戳
   * 使用场景：需要持久化任务结果的场景

   ### 3. 子任务相关 Handler

   DefaultAggregateSubTaskResults* 功能：聚合所有子任务的结果，计算统计信息

   * 输出统计：
   * 总任务数 (total)
   * 成功数 (success_count)
   * 失败数 (failed_count)
   * 成功率 (success_rate)
   * 总数据量 (total_data)
   * 是否达到阈值 (meets_threshold)
   * 配置参数：
   * success_rate_threshold: 成功率阈值（默认80.0%）
   * 使用场景：父任务需要统计子任务执行情况

   DefaultBatchGenerateSubTasks* 功能：限制一次性生成的子任务数量，分批生成

   * 配置参数：
   * batch_size: 每批生成数量（默认10）
   * 使用场景：需要生成大量子任务时，避免一次性生成过多

   ### 4. 参数校验 Handler

   DefaultValidateParams* 功能：校验任务参数的完整性和合法性

   * 校验内容：
   * 必需参数检查
   * 自定义校验规则
   * 配置参数：
   * required_params: 必需参数列表（[]string）
   * param_validators: 参数校验规则（map[string]func(interface{}) error）
   * 使用场景：需要确保参数完整性和合法性的任务

   ### 5. 补偿逻辑 Handler

   DefaultCompensate* 功能：执行补偿逻辑，回滚已执行的子任务

   * 执行方式：逆序执行补偿动作（类似回滚）
   * 配置参数：
   * compensate_actions: 补偿动作列表（[]func(*TaskContext) error）
   * 使用场景：任务失败后需要回滚的场景

   ### 6. 缓存相关 Handler

   DefaultSkipIfCached* 功能：检查缓存，如果命中则跳过任务执行

   * 配置参数：
   * cache_key: 缓存键（默认使用任务ID）
   * cache_ttl: 缓存有效期（秒）
   * 使用场景：避免重复执行相同任务

   ### 7. 重试增强 Handler

   DefaultRetryOnFailure* 功能：失败时自动重试（增强版，支持自定义重试策略）

   * 配置参数：
   * max_retries: 最大重试次数
   * retry_delay: 重试延迟（秒，支持指数退避）
   * 使用场景：需要更灵活重试策略的任务

   ### 8. 通知 Handler

   DefaultNotifyOnFailure* 功能：任务失败时发送通知

   * 支持渠道：邮件、短信、Webhook等
   * 配置参数：
   * notification_channels: 通知渠道列表（[]string）
   * 使用场景：关键任务失败时需要及时通知
5. 父任务结果聚合：可以在Task字段中新增一个hasSubTask

   1. 值为1时，父任务的handler会延迟执行，等到所有子任务都被执行了或无法执行下去才会触发
6. Workflow也需要支持基于状态的回调
7. 补充缓存机制：上游任务的结果可以缓存起来（基于接口，默认提供内存缓存)，下游任务可以获取到
8. DAG更新：添加SubTask后，DAG的结构也要更新。需要构建新的边，连接父任务 - 子任务 - 下游任务，并删除连接父任务和下游子任务的边。潜在优化：下游任务是否真的需要等所有上游子任务完成才可以启动？还是可以在部分任务完成的情况下就可以启动了
9. 支持在非事务模式下开启partial success支持。父任务带有一个errorTolerance, 允许一定比例的子任务失败。默认是0。
10. 支持简单的数据统计功能
