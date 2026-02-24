package errors

import (
	"fmt"
)

type ErrorCode string

const (
	ErrCodeConnection     ErrorCode = "CONNECTION_ERROR"
	ErrCodeTimeout        ErrorCode = "TIMEOUT_ERROR"
	ErrCodeNotFound       ErrorCode = "NOT_FOUND"
	ErrCodeAlreadyExists  ErrorCode = "ALREADY_EXISTS"
	ErrCodeInvalidInput   ErrorCode = "INVALID_INPUT"
	ErrCodeInternal       ErrorCode = "INTERNAL_ERROR"
	ErrCodeLockFailed     ErrorCode = "LOCK_FAILED"
	ErrCodeSerialization  ErrorCode = "SERIALIZATION_ERROR"
	ErrCodeRetryExhausted ErrorCode = "RETRY_EXHAUSTED"
)

type RedisError struct {
	Code    ErrorCode
	Message string
	Err     error
	Context map[string]interface{}
}

func (e *RedisError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *RedisError) Unwrap() error {
	return e.Err
}

func (e *RedisError) WithContext(key string, value interface{}) *RedisError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

func New(code ErrorCode, message string) *RedisError {
	return &RedisError{
		Code:    code,
		Message: message,
	}
}

func Wrap(err error, code ErrorCode, message string) *RedisError {
	return &RedisError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func IsNotFound(err error) bool {
	if redisErr, ok := err.(*RedisError); ok {
		return redisErr.Code == ErrCodeNotFound
	}
	return false
}

func IsTimeout(err error) bool {
	if redisErr, ok := err.(*RedisError); ok {
		return redisErr.Code == ErrCodeTimeout
	}
	return false
}

func IsConnectionError(err error) bool {
	if redisErr, ok := err.(*RedisError); ok {
		return redisErr.Code == ErrCodeConnection
	}
	return false
}
