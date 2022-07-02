package retry

import (
	"errors"
	"strings"

	githubMysql "github.com/go-sql-driver/mysql"
)

type IErrorMatcher interface {
	MatchWith(err error) bool
}

type ErrorMatcherFunc func(err error) bool

func NewErrorMatcherFunc(f func(err error) bool) ErrorMatcherFunc {
	return f
}

func (f ErrorMatcherFunc) MatchWith(err error) bool {
	return f(err)
}

func DuplicateEntryErrorMatcher(duplicateEntryName string) IErrorMatcher {
	return NewErrorMatcherFunc(func(err error) bool {
		return IsDuplicateEntryError(err, duplicateEntryName)
	})
}

// IsDuplicateEntryError reference from https://github.com/go-gorm/gorm/issues/4037
func IsDuplicateEntryError(err error, duplicateEntryName string) bool {
	githubError := new(githubMysql.MySQLError)
	if errors.As(err, &githubError) && githubError.Number == 1062 {
		return strings.Contains(githubError.Message, duplicateEntryName)
	}

	return false
}

func ErrorIs(targetError error) ErrorMatcherFunc {
	return NewErrorMatcherFunc(func(err error) bool {
		return err == targetError
	})
}
