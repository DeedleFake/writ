package writ

import "deedles.dev/writ/syntax"

func errMsg(msg string) *syntax.Error { return syntax.ErrorMsg(msg) }

func errf(format string, args ...any) *syntax.Error { return syntax.Errorf(format, args...) }

func errAt(start, end int, msg string) *syntax.Error { return syntax.ErrorAt(start, end, msg) }

func asError(err error) *syntax.Error { return syntax.AsError(err) }
