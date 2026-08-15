// Package apperr 定义业务错误，携带 HTTP 状态码与业务 code，
// 供 service 层返回、handler/response 层统一映射为响应体。
package apperr

// Error 是带 HTTP 状态码与业务错误码的错误。
type Error struct {
	HTTPStatus int
	Code       int
	Message    string
}

func (e *Error) Error() string {
	return e.Message
}

// New 构造一个业务错误。
func New(httpStatus, code int, message string) *Error {
	return &Error{HTTPStatus: httpStatus, Code: code, Message: message}
}
