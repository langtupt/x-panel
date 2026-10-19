// Package apperr carries an HTTP status and a user-facing message from
// business logic up to the handlers.
package apperr

import (
	"fmt"
	"net/http"
)

type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func New(status int, format string, args ...any) *Error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, args...)}
}

func Invalid(format string, args ...any) *Error   { return New(http.StatusBadRequest, format, args...) }
func Forbidden(format string, args ...any) *Error { return New(http.StatusForbidden, format, args...) }
func NotFound(format string, args ...any) *Error  { return New(http.StatusNotFound, format, args...) }
func Conflict(format string, args ...any) *Error  { return New(http.StatusConflict, format, args...) }
func Agent(err error) *Error {
	return New(http.StatusBadGateway, "lỗi khi thực thi trên máy chủ: %v", err)
}
