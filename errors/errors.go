package errors

import "fmt"

type Kind int

const (
	KindUser Kind = iota + 1
	KindProvider
	KindDiff
	KindBackground
	KindInternal
	KindCancelled
)

type Error struct {
	Kind  Kind
	Code  string
	Msg   string
	Hint  string
	Cause error
}

func (e *Error) Error() string {
	msg := e.Msg
	if e.Cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Cause)
	}
	if e.Hint != "" {
		return fmt.Sprintf("%s\nHint: %s", msg, e.Hint)
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }

func User(code, msg, hint string) *Error {
	return &Error{Kind: KindUser, Code: code, Msg: msg, Hint: hint}
}

func Provider(code, msg, hint string, cause error) *Error {
	return &Error{Kind: KindProvider, Code: code, Msg: msg, Hint: hint, Cause: cause}
}

func Diff(code, msg, hint string) *Error {
	return &Error{Kind: KindDiff, Code: code, Msg: msg, Hint: hint}
}

func Internal(code, msg string, cause error) *Error {
	return &Error{Kind: KindInternal, Code: code, Msg: msg, Cause: cause}
}

func Background(code, msg, hint string, cause error) *Error {
	return &Error{Kind: KindBackground, Code: code, Msg: msg, Hint: hint, Cause: cause}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*Error); ok {
		return int(e.Kind)
	}
	return int(KindInternal)
}
