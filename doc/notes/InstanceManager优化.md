# Instance Manager重构设计

我想设计包含一个二维任务队列的Instance Manager。二维任务队列的列方向是任务的level, 行方向是任务。队列来自于对任务有向无环图的拓扑排序。level越大，意味着需要越多的任务依赖。任务队列可以被设计为[]container.List,或者[]map[string]Task。有一个currentLevel计数器决定当前的执行进度。任务可能是模板任务或者真实任务。模板任务负责生成真实任务， 准备真实任务所依赖的数据， 并注入到真实任务的参数字段。真实任务绑定了一个具有业务含义的job function，可以执行一些操作。所有任务都可以绑定若干个statusHandlers，可以在对应的状态下触发并按顺序执行。现在我有一个调度器和一个执行器。调度器负责管理任务队列状态，派发任务。执行器负责执行job function和handlers，还有通知调度器任务的状态。在调度器中，有以下goroutine:

1. queueManagerGoroutine:
   1. 职责：
      1. 负责维护队列状态
      2. 负责维护currentLevel计数器
      3. 负责向队列增加任务
         1. 子任务
         2. 失败且没有超过最大重试次数的任务
   2. 监听：
      1. taskStatusChannel
         1. 模板任务成功
         2. 模板任务失败
         3. 子任务成功
         4. 子任务失败
2. taskSubmissionGoroutine:
   1. 根据currentLevel选定任务队列，根据maxTaskSubmission从队列取出任务提交给Executor. 提交的task的TaskID会发送到taskObserverChannel. 当任务队列为空时，会挂起等待；当currentLevel > len(taskQueue)时结束
3. taskObserverGoroutine:
   1. 监听从Executor发来的任务状态（taskStatusChannel)，维护任务状态统计
      1. 总任务数 = 静态任务数 + 子任务数 = 成功任务数 + 失败任务数 + 等待任务数
      2. 静态任务数
      3. 子任务数
      4. 成功任务数
      5. 失败任务数
      6. 等待任务数

问题：什么时候currentLevel应该+1？

1. 显然，当currentLevel指向的队列为空，且没有更多任务可以添加到队列的时候
   1. 当len(queue[currentLevel]) == 0 && templateTaskCount == 0时
2. 那么如何知道没有更多任务可以被添加到队列呢？
   1. 所有被标记为template的任务被真正执行且成功的时候
   2. 那么如何知道这些template任务都被执行了呢？
   3. 我们可以维护一个templateTaskCount的计数器，归0的时候就可以判断都执行完了
      1. 我们可以在任务队列初始化时，知道当前level有多少个templateTask
      2. 什么时候templateTask可以 -1?
         1. templateTask被提交到Executor, 在Executor中会调用Instance Manager的AddSubTask方法。AddSubTask方法会把新的子任务添加到当前队列。
         2. 模板任务的handler怎么处理？
            1. 子任务的handler会继承模板任务的handler
            2. 不执行模板任务的handler，避免产生潜在的冲突
         3. 当AddSubTask执行成功时，执行notifySuccess
         4. taskObserverGoroutine会收到通知，转发给queueManagerGoroutine
         5. queueManagerGoroutine会执行atomic.AddInt32(&templateTaskCount, -1)
      3. 异常情况：如何保证templateTaskCount不小于0？
         1. 兜底：万一出现只能panic, 排查原因
   4. 问题：模板任务能否产生模板任务呢？为了简化，我们禁止，并在AddSubTask被调用时执行静态检查。
      1. 在AddSubTask中，如果新产生的任务被标记template则返回RecursiveTemplateForbidden错误
      2. 问题：模板任务的job function是用户定义的，如何防止因疏忽或者故意产生多重sub task?
         1. AddSubTask会在childrenParentMapping添加记录, 这样会获取到parent，如果parent和children都是template就报错
