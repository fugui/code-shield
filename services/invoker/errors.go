package invoker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrorClass is a stable classification used by retry, split and observability
// policies. It must be attached at the layer that observes the failure, not by
// scanning arbitrary error text in engine code.
type ErrorClass string

const (
	ErrorClassNone                ErrorClass = "none"
	ErrorClassCanceled            ErrorClass = "canceled"
	ErrorClassTimeout             ErrorClass = "timeout"
	ErrorClassIdleTimeout         ErrorClass = "idle_timeout"
	ErrorClassContractMismatch    ErrorClass = "contract_mismatch"
	ErrorClassRateLimited         ErrorClass = "rate_limited"
	ErrorClassNetworkTransient    ErrorClass = "network_transient"
	ErrorClassResourceBusy        ErrorClass = "resource_busy"
	ErrorClassResourceUnavailable ErrorClass = "resource_unavailable"
	ErrorClassAuth                ErrorClass = "auth"
	ErrorClassConfig              ErrorClass = "config"
	ErrorClassContentFiltered     ErrorClass = "content_filtered"
	ErrorClassOutputMissing       ErrorClass = "output_missing"
	ErrorClassOutputInvalid       ErrorClass = "output_invalid"
	ErrorClassJSONInvalid         ErrorClass = "json_invalid"
	ErrorClassUnknown             ErrorClass = "unknown"
)

// ClassifiedError carries a stable failure class through wrapping layers while
// preserving both the diagnostic message and the original cause.
type ClassifiedError struct {
	Class ErrorClass
	cause error
	msg   string
}

func NewClassifiedError(class ErrorClass, format string, args ...any) *ClassifiedError {
	return &ClassifiedError{Class: class, msg: fmt.Sprintf(format, args...)}
}

func WrapClassifiedError(class ErrorClass, cause error, format string, args ...any) *ClassifiedError {
	if cause == nil {
		return NewClassifiedError(class, format, args...)
	}
	message := fmt.Sprintf(format, args...)
	if message == "" {
		message = cause.Error()
	}
	return &ClassifiedError{Class: class, cause: cause, msg: message}
}

func (e *ClassifiedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause != nil && e.msg != e.cause.Error() {
		return fmt.Sprintf("%s: %s", e.msg, e.cause.Error())
	}
	return e.msg
}

func (e *ClassifiedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *ClassifiedError) ErrorClass() ErrorClass {
	if e == nil {
		return ErrorClassUnknown
	}
	return e.Class
}

// ClassifyError returns a stable class for typed failures and standard context
// failures. Untyped errors remain Unknown; source layers are responsible for
// attaching classes when they have enough evidence.
func ClassifyError(err error) ErrorClass {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Class
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorClassCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorClassTimeout
	default:
		return ErrorClassUnknown
	}
}

func errorClassForHTTPStatus(status int) ErrorClass {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorClassAuth
	case http.StatusNotFound:
		return ErrorClassConfig
	case http.StatusTooManyRequests:
		return ErrorClassRateLimited
	default:
		if status >= 500 {
			return ErrorClassNetworkTransient
		}
		return ErrorClassUnknown
	}
}
