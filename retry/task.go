package retry

// ITask extract an interface to mock code for writing unit test
//go:generate mockgen -destination ./task_mock.go -package retry -source task.go ITask
type ITask interface {
	Exec() error
}

func NewTaskFunc(f func() error) ITask {
	return TaskFunc(f)
}

type TaskFunc func() error

func (f TaskFunc) Exec() error {
	return f()
}
