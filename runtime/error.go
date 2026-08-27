package runtime

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

func (e *Error) withFile(file string) *Error {
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

func errAtf(start, end int, format string, args ...any) *Error {
	e := errAt(start, end, fmt.Sprintf(format, args...))
	return e
}

func errVal(v Value, msg string) *Error {
	s, e := v.srcSpan().Start, v.srcSpan().End
	if e == 0 && s == 0 && !v.HasSpan() {
		return errMsg(msg)
	}
	return errAt(s, e, msg)
}

func errValf(v Value, format string, args ...any) *Error {
	return errVal(v, fmt.Sprintf(format, args...))
}

func asError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return errMsg(err.Error())
}

// ErrorMsg builds an error with no source span.
func ErrorMsg(msg string) *Error { return errMsg(msg) }

// Errorf builds a formatted error with no source span.
func Errorf(format string, args ...any) *Error { return errf(format, args...) }

// ErrorAt builds an error covering a byte range.
func ErrorAt(start, end int, msg string) *Error { return errAt(start, end, msg) }

// ErrorIncomplete is [ErrorAt] for a source prefix that needs more input.
func ErrorIncomplete(start, end int, msg string) *Error {
	e := errAt(start, end, msg)
	e.incomplete = true
	return e
}

// IsIncomplete reports whether this is an incomplete-parse error.
func (e *Error) IsIncomplete() bool { return e != nil && e.incomplete }

// AsError converts err to *Error.
func AsError(err error) *Error { return asError(err) }

// WithFile sets File when it is empty.
func (e *Error) WithFile(file string) *Error { return e.withFile(file) }
