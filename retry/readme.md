# retry 包的实现 (抽象 && 单测)

## 背景

ticket 服务内有一张 `ticket_meta` 表，这个 meta 表里面有两个唯一索引，分别为 `uniq_ticket_id` 与 `uniq_biz_id_source`

```SQL
CREATE TABLE `ticket_meta`
(
    `id`               bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '自增ID',
    `ticket_order_id` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT '受理单ID',
    `ticket_status`   tinyint                                                      NOT NULL DEFAULT '1' COMMENT '受理单状态：1-未受理，10-受理中，99-受理完成',
    `biz_source`       tinyint                                                      NOT NULL DEFAULT '-100' COMMENT '上游业务数据来源：1-客服',
    `biz_id`           varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT '业务数据唯一ID，如客服工单ID',
    `remark`           text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT '备注',
    `parent_order_id`  varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT '关联上级受理单ID',
    `resource_id`      varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT '关联资源ID',
    `is_test`          tinyint                                                      NOT NULL DEFAULT '0' COMMENT '0表示正常，1表示测试数据，2表示试用数据，3表述全链路测试',
    `delete_time`      datetime                                                              DEFAULT NULL COMMENT '数据软删除时间',
    `create_time`      datetime                                                     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `update_time`      datetime                                                     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_ticket_id` (`ticket_order_id`),
    UNIQUE KEY `uniq_biz_id_source` (`biz_id`,`biz_source`)
) ENGINE=InnoDB AUTO_INCREMENT=670 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='受理元数据表';
```

在每次插入 `ticket_meta` 记录时，会使用一个 hash 算法生成一个 `ticket_order_id` 的字符串，由于采用的 hash，因此 `ticket_order_id` 会存在重复生成的可能性，虽然这个可能性挺低的，biz_id 和 biz_source 由上游传递而来，是用来做幂等的，以保证相同的 biz_id 和 biz_source，表内只会有一条记录。

现在上游过来一条 biz 数据，里面携带了 biz_id 和 biz_source，我们需要在 ticket_meta 里面创建一条对应的 ticket_meta 数据。如果表里面已经有该 biz_id 的数据了，则不必创建了。即完成一个 CreateIfNotExist() 的语义。

假设有两个相同的 biz 数据几乎同时到达，则第一个先到的数据会插入成功，而第二个数据再往 ticket_meta 插入记录时，会由于 uniq_biz_id_source 的唯一性约束触发 duplicate entry 错误而失败。所以理论上我们只需要忽略 duplicate entry 错误就行，因为这代表 ticket_meta 数据已经生成了。

但是我们不能忽略所有的  duplicate entry 错误，如果 duplicate entry error 是由于 uniq_ticket_id 的唯一性约束引发的，这代表我们生成了重复的 `ticket_order_id`，对于这种错误，我们不能忽略，而是需要重试。

此处涉及到了重试，这就是我们 retry 包的起源了。

## 实现

### 初版: 很具体的实现

核心逻辑如下

```Go
func CreateTicketMetaWithRetry() error {
   const retryTimes = 3
   for i := 0; i < retryTimes; i++ {
      meta := GenerateTicketMeta()
      err := InsertTicketMeta(meta)
      if err != nil {
         if IsDuplicatedBizIdError(err) {
            // 允许 biz_id 重复, 直接返回 nil
            return nil
         }
         if IsDuplicatedTicketIdError(err) {
            // ticket id 冲突则需要重试
            continue
         }
      } else {
         return nil
      }
   }
   // ...
}
```

这是一个很具体的实现，它可以很完美的解决我们上面碰到的问题，但是也和上面的问题死死绑定在了一起。

### 加一点抽象

上面场景涉及到三个对象

- InsertTicketMeta

- DuplicatedBizIdError

- DuplicatedTicketIdError

开始抽象

- 将 InsertTicketMeta  抽象为一个 Task，这个 Task 会返回一个 error，函数签名大概是 `func() error`

- 将 DuplicatedBizIdError 进一步泛化为 AcceptableError ，函数签名大概是 `func(err error) bool` ，它接受一个 err，返回一个 bool，当 Task 的执行结果是一个 AcceptableError 时，我们就可以忽略它直接返回

- 同理，将 DuplicatedTicketIdError 也泛化为 NeedRetryError，当 Task 的执行结果是一个 AcceptableError 时，我们需要重试

所以现在代码就变成了

```Go
func CreateTicketMetaWithRetry() error {
   task := func() error {
      meta := GenerateTicketMeta()
      return InsertTicketMeta(meta)
   }
   return DoTaskWithRetry(task, IsDuplicatedBizIdError, IsDuplicatedTicketIdError)
}

func DoTaskWithRetry(task func() error, acceptableError, needRetryError func(err error) bool) error {
   const retryTimes = 3
   for i := 0; i < retryTimes; i++ {
      err := task()
      if err != nil {
         if acceptableError(err) {
            // 允许 biz_id 重复, 直接返回 nil
            return nil
         }
         if needRetryError(err) {
            // ticket id 冲突则需要重试
            continue
         }
      } else {
         return nil
      }
   }
   // ...
}
```

### 加一点面向对象

我本人不太喜欢函数签名里面有太多的参数，我们可以把上面的 task, acceptableError, needRetryError 都封装在对象里。让这个对象对外暴露一个 Run() 方法，用于执行上面的核心 retry 逻辑。所以现在代码变成了这样。

```Go
type Entry struct {
   AcceptableError func(err error) bool // 可接受的 error
   NeedRetryError  func(err error) bool // 需要重试的 error
   Task            func() error         // 需要做的事情
   Times           int                  // 重试次数
}

func (m *Entry) Run() error {
   for i := 0; i < m.Times; i++ {
      err := m.Task()
      if err != nil {
         if m.AcceptableError(err) {
            // 允许 biz_id 重复, 直接返回 nil
            return nil
         }
         if m.NeedRetryError(err) {
            // ticket id 冲突则需要重试
            continue
         }
      } else {
         return nil
      }
   }
   // ...
}

func CreateTicketMetaWithRetry() error {
   task := func() error {
      meta := GenerateTicketMeta()
      return InsertTicketMeta(meta)
   }
   retryEntry := &Entry{
      AcceptableError: IsDuplicatedBizIdError,
      NeedRetryError:  IsDuplicatedTicketIdError,
      Task:            task,
      Times:           3,
   }
   return retryEntry.Run()
}
```

这样用户只需要提前构造后一个 retry.Entry 对象，然后执行 Run() 方法就好了。

给这个对象取名为 Entry 其实并没有什么特别的含义，Entry 是入口的意思，package name 本身就为 retry，因此 retry.Entry 也就是 retry 的入口的意思了，暗示该 package 提供的功能都是通过 Entry 对象来完成的。

### 进一步抽象

很容易想到，用户可能有多个 AcceptableError 和 NeedRetryError。因此我们使用一个数组来保存 AcceptableError 和 NeedRetryError

```Go
type Entry struct {
   AcceptableErrors []func(err error) bool // 可接受的 error
   NeedRetryErrors  []func(err error) bool // 需要重试的 error
   Task             func() error           // 需要做的事情
   Times            int                    // 重试次数
}

func (m *Entry) Run() error {
   for i := 0; i < m.Times; i++ {
      err := m.Task()
      if err != nil {
         for _, acceptableError := range m.AcceptableErrors {
            if acceptableError(err) {
               // 忽略
               return nil
            }
         }
         for _, needRetryError := range m.NeedRetryErrors {
            if needRetryError(err) {
               // 重试
               continue
            }
         }
      } else {
         return nil
      }
   }
   // ...
}
```

### 加一点单测

到目前为止，代码都还挺容易理解的。现在问题来了，该怎么为这段代码写单测。

毕竟核心的 Run() 里面涉及 for 循环，涉及 error 非空的逻辑分支判断，涉及 acceptableError 和 needRetryError 的匹配...... 没有单测的话，其实我心里挺没有底的。

但是怎么为它添加单测却又是让人头疼的事情。

第一直接时，我们可以编写一个 MockTask() 的方法，让其返回一个**固定的** error。

```Go
func MockTask() error {
   return errors.New("a acceptable error")
}
```

利用这个 *`MockTask`*`()` 来进行单测

```SQL
func TestEntry_Run(t *testing.T) {
   entry := Entry{
      AcceptableErrors: []func(err error) bool{
         func(err error) bool {
            return err.Error() == "a acceptable error"
         },
      },
      NeedRetryErrors: nil,
      Task:            MockTask,
      Times:           0,
   }
   err := entry.Run()
   // 由于是一个 acceptable error, 因此这里用 NoError 来断言
   assert.NoError(t, err)
}

func MockTask() error {
   return errors.New("a acceptable error")
}
```

但是这个 MockTask() 有一个致命的缺陷，它只有一个固定的返回值。

如果需要测试 Task() 第一次返回一个 NeedRetry Error，第二次返回一个 Acceptable Error，MockTask() 没办法做到。

当然，我们可以使用一个更高阶的闭包来达到这个效果。

```Go
func MockTaskGenerator() func() error {
   i := 0
   return func() error {
      i++
      return errors.New(strconv.Itoa(i))
   }
}
```

可以利用 MockTaskGenerator 方法来生成一个 MockTask() 对象，每次调用 MockTask() 对象时都将得到一个不同的返回值。

但是这种方式每次都需要构建一个高阶闭包，对单测编写者不友好。

于是就想到了 gomock，gomock 可以根据接口来生成 mock 对象，并且可以很方便的指定 mock 对象的方法每一次的执行结果。

举个栗子:

```Go
//go:generate mockgen ...
type StringGenerator interface {
   GenerateString() string
}

func TestName(t *testing.T) {
   ctrl := gomock.NewController(t)
   generator := NewMockStringGenerator(ctrl)

   // 指定返回值
   generator.EXPECT().GenerateString().Return("hello")
   generator.EXPECT().GenerateString().Return("world")

   // 打印返回值
   fmt.Println(generator.GenerateString()) // hello
   fmt.Println(generator.GenerateString()) // world
}
```

因此就把 `func Task() error` 抽象成了一个接口

```Go
type ITask interface {
   Exec() error
}

type Entry struct {
   AcceptableErrors []func(err error) bool // 可接受的 error
   NeedRetryErrors  []func(err error) bool // 需要重试的 error
   Task             ITask                  // 需要做的事情
   Times            int                    // 重试次数
}
```

使用 gomock 根据 ITask 生成一个  MockTask 对象，就可以很方便的写单测了！

以下为单测代码，基本将每一种 case 都覆盖到了

```Go
func TestEntry_Run(t *testing.T) {
   ctrl := gomock.NewController(t)
   defer ctrl.Finish()
   mockTask := NewMockITask(ctrl)

   error1 := errors.New("error1")
   error2 := errors.New("error2")
   error3 := errors.New("error3")

   // 调用链 a.b().c()
   entry := NewEntry(mockTask, 3).
      WithAcceptableErrors(ErrorIs(error1)).
      WithRetryErrors(ErrorIs(error2))

   // case1: 没有出现 error
   mockTask.EXPECT().Exec().Return(nil)
   err := entry.Run()
   assert.NoError(t, err)

   // case2: 返回 error1
   mockTask.EXPECT().Exec().Return(error1)
   err = entry.Run()
   assert.NoError(t, err)

   // case3: 返回 error2, 重试之后返回 nil
   mockTask.EXPECT().Exec().Return(error2)
   mockTask.EXPECT().Exec().Return(nil)
   err = entry.Run()
   assert.NoError(t, err)

   // case4: 返回 error2, 重试之后返回 error1
   mockTask.EXPECT().Exec().Return(error2)
   mockTask.EXPECT().Exec().Return(error1)
   err = entry.Run()
   assert.NoError(t, err)

   // case5: 连续三次都返回 error2, 已达最大重试次数, 因此会返回 error2
   mockTask.EXPECT().Exec().Return(error2).Times(3)
   err = entry.Run()
   assert.ErrorIs(t, err, error2)

   // case6: 返回 error3
   mockTask.EXPECT().Exec().Return(error3)
   err = entry.Run()
   assert.ErrorIs(t, err, error3)

   // case7: 返回 error2 之后返回 error3
   mockTask.EXPECT().Exec().Return(error2)
   mockTask.EXPECT().Exec().Return(error3)
   err = entry.Run()
   assert.ErrorIs(t, err, error3)
}
```

### function 与 interface 的适配

用户可能希望直接传一个 func() error 来作为 Task，但是我们 Task 的签名为 ITask ，func() error  是没办法直接转换为 ITask 的，为了便于用户使用，我们需要提供一个适配器 (adapter)。

使用如下几步，可以轻松得到一个从 function 到 interface 的 adapter

1. 为 function 建一个 type

```Go
type TaskFunc func() error
```

1. 让该 type implement 目标 interface (ITask)

```Go
type ITask interface {
   Exec() error
}

func (f TaskFunc) Exec() error {
   return f()
}
```

1. 做一个构造函数吧，把原始的 function 转换为我们新定义的 func type

```Go
func NewTaskFunc(f func() error) ITask {
   return TaskFunc(f)
}
```

### 总结与反思

retry 包写到最后，的确是弯弯绕绕太多，有点折磨后面 review 代码的人。(PS: 文档中有些抽象过程是直接在脑中进行的，当时并没有落下对应代码，这里为了表述清楚，所以额外写了一下中间代码)

我思考了一下原因，原因就是标题中提到的两个点：抽象 + 单测。

抽象将一个具体的 function 泛化成了代表一类事物的 functor，而为了方便单测，又从 functor 中提取出了一个 interface。

但是抽象和单测我也认为是这个包的优点所在。我自身对这个包比较满意，主要是对单测很满意。刚跑了一下 go test coverage，这个包的单测覆盖率是百分之九十多。