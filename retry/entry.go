package retry

type Entry struct {
	AcceptableErrors []IErrorMatcher // 可接受的 error 列表
	NeedRetryErrors  []IErrorMatcher // 需要重试的 error 列表
	Task             ITask           // 需要做的事情
	Times            int             // 重试次数
}

func NewEntry(task ITask, times int) *Entry {
	return &Entry{
		Task:  task,
		Times: times,
	}
}

func (m *Entry) WithAcceptableErrors(matchers ...IErrorMatcher) *Entry {
	m.AcceptableErrors = matchers
	return m
}

func (m *Entry) WithRetryErrors(matchers ...IErrorMatcher) *Entry {
	m.NeedRetryErrors = matchers
	return m
}

// Result 表示 Run 的执行结果
// 不要用 LastErr 是否为 nil 来判断是否执行成功
// 用 Success 来判断是否执行成功
// 当 Success 为 false 时, LastErr 肯定不为 nil
// 详细使用方法可见 TestEntry_Run()
type Result struct {
	// 是否执行成功
	Success bool
	// 执行过程中碰到的最后一个 error
	LastErr error
}

func (r *Result) LastErrMatchWith(matcher IErrorMatcher) bool {
	return matcher.MatchWith(r.LastErr)
}

func (m *Entry) Run() *Result {
	var err error
	for i := 0; i < m.Times; i++ {
		err = m.Task.Exec()
		if err != nil {
			for _, acceptableError := range m.AcceptableErrors {
				if acceptableError.MatchWith(err) {
					return &Result{
						Success: true,
						LastErr: err,
					}
				}
			}

			needRetry := false
			for _, retryError := range m.NeedRetryErrors {
				if retryError.MatchWith(err) {
					needRetry = true
					break
				}
			}

			if !needRetry {
				return &Result{
					Success: false,
					LastErr: err,
				}
			}
		} else {
			return &Result{
				Success: true,
				LastErr: nil,
			}
		}
	}
	return &Result{
		Success: false,
		LastErr: err,
	}
}
