package ir

import (
	"fmt"

	"deedles.dev/writ/syntax"
)

// Error is a parse error for IR construction. Start and End are byte
// offsets when known.
type Error struct {
	Start   int
	End     int
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func errMsg(msg string) *Error {
	return &Error{Message: msg}
}

func errf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

func errAt(start, end int, msg string) *Error {
	if end < start {
		end = start
	}
	if end == start {
		end = start + 1
	}
	return &Error{Start: start, End: end, Message: msg}
}

func errForm(v syntax.Form, msg string) *Error {
	if sp, ok := v.Span(); ok {
		return errAt(sp.Start, sp.End, msg)
	}
	return errMsg(msg)
}

func errFormf(v syntax.Form, format string, args ...any) *Error {
	return errForm(v, errf(format, args...).Message)
}
