package retry

import (
	"errors"
	githubMysql "github.com/go-sql-driver/mysql"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestEntry_Run(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockTask := NewMockITask(ctrl)

	error1 := errors.New("error1")
	error2 := errors.New("error2")
	error3 := errors.New("error3")

	entry := NewEntry(mockTask, 3).
		WithAcceptableErrors(ErrorIs(error1)).
		WithRetryErrors(ErrorIs(error2))

	// case1: 没有出现 error
	mockTask.EXPECT().Exec().Return(nil)
	result := entry.Run()
	assert.True(t, result.Success)
	assert.NoError(t, result.LastErr)

	// case2: 返回 error1
	mockTask.EXPECT().Exec().Return(error1)
	result = entry.Run()
	assert.True(t, result.Success)
	assert.ErrorIs(t, result.LastErr, error1)

	// case3: 返回 error2, 重试之后返回 nil
	mockTask.EXPECT().Exec().Return(error2)
	mockTask.EXPECT().Exec().Return(nil)
	result = entry.Run()
	assert.True(t, result.Success)
	assert.ErrorIs(t, result.LastErr, nil)

	// case4: 返回 error2, 重试之后返回 error1
	mockTask.EXPECT().Exec().Return(error2)
	mockTask.EXPECT().Exec().Return(error1)
	result = entry.Run()
	assert.True(t, result.Success)
	assert.ErrorIs(t, result.LastErr, error1)

	// case5: 连续三次都返回 error2, 已达最大重试次数, 因此会返回 error2
	mockTask.EXPECT().Exec().Return(error2).Times(3)
	result = entry.Run()
	assert.False(t, result.Success)
	assert.ErrorIs(t, result.LastErr, error2)

	// case6: 返回 error3
	mockTask.EXPECT().Exec().Return(error3)
	result = entry.Run()
	assert.False(t, result.Success)
	assert.ErrorIs(t, result.LastErr, error3)

	// case7: 返回 error2 之后返回 error3
	mockTask.EXPECT().Exec().Return(error2)
	mockTask.EXPECT().Exec().Return(error3)
	result = entry.Run()
	assert.False(t, result.Success)
	assert.ErrorIs(t, result.LastErr, error3)
}

func TestResult_LastErrMatchWith(t *testing.T) {
	type fields struct {
		Success bool
		LastErr error
	}
	type args struct {
		matcher IErrorMatcher
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "Duplicate entry error hello",
			fields: fields{
				Success: false,
				LastErr: &githubMysql.MySQLError{
					Number:  1062,
					Message: "Duplicate entry '111' for key 'hello'",
				},
			},
			args: args{
				matcher: DuplicateEntryErrorMatcher("hello"),
			},
			want: true,
		}, {
			name: "nil error",
			fields: fields{
				Success: true,
				LastErr: nil,
			},
			args: args{
				matcher: DuplicateEntryErrorMatcher("hello"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{
				Success: tt.fields.Success,
				LastErr: tt.fields.LastErr,
			}
			assert.Equalf(t, tt.want, r.LastErrMatchWith(tt.args.matcher), "LastErrMatchWith(%v)", tt.args.matcher)
		})
	}
}
