package syntax

import "fmt"

// Error is a parse, type, or evaluation error. Start and End are byte
// offsets into the source when known.
type Error struct {
	File       string
	Start      int
	End        int
	Message    string
	incomplete bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.File != "" {
		return e.File + ": " + e.Message
	}
	return e.Message
}

// WithFile sets File when it is empty.
func (e *Error) WithFile(file string) *Error {
	if e == nil {
		return nil
	}
	if e.File != "" || file == "" {
		return e
	}
	cp := *e
	cp.File = file
	return &cp
}

// IsIncomplete reports whether this is an incomplete-parse error.
func (e *Error) IsIncomplete() bool { return e != nil && e.incomplete }

// ErrorMsg builds an error with no source span.
func ErrorMsg(msg string) *Error {
	return &Error{Message: msg}
}

// Errorf builds a formatted error with no source span.
func Errorf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// ErrorAt builds an error covering a byte range.
func ErrorAt(start, end int, msg string) *Error {
	if end < start {
		end = start
	}
	if end == start {
		end = start + 1
	}
	return &Error{Start: start, End: end, Message: msg}
}

// ErrorIncomplete is [ErrorAt] for a source prefix that needs more input.
func ErrorIncomplete(start, end int, msg string) *Error {
	e := ErrorAt(start, end, msg)
	e.incomplete = true
	return e
}

// AsError converts err to *Error.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return ErrorMsg(err.Error())
}
