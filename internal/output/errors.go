package output

import "fmt"

// Exit codes (see README "Exit codes"). Stable contract — do not renumber.
const (
	ExitOK         = 0
	ExitError      = 1  // generic / unexpected
	ExitUsage      = 2  // bad flags/args
	ExitUnauth     = 3  // 401
	ExitForbidden  = 4  // 403
	ExitNotFound   = 5  // 404
	ExitConflict   = 6  // 409
	ExitValidation = 7  // local validation error
	ExitIntegrity  = 8  // content-hash mismatch
	ExitQuota      = 9  // 413 / quota exceeded
	ExitNetwork    = 10 // transport error
)

// CLIError carries an exit code plus a machine code and optional remediation hint.
type CLIError struct {
	Code     int    // process exit code (Exit*)
	CodeName string // stable machine code for --json, e.g. "not_found"
	Message  string
	Hint     string
	Err      error // wrapped cause (not shown to users directly)
}

func (e *CLIError) Error() string { return e.Message }
func (e *CLIError) Unwrap() error { return e.Err }

// Errorf builds a CLIError.
func Errorf(code int, codeName, hint, format string, a ...any) *CLIError {
	return &CLIError{Code: code, CodeName: codeName, Hint: hint, Message: fmt.Sprintf(format, a...)}
}

// Usage is a convenience for argument/flag errors (exit 2).
func Usage(format string, a ...any) *CLIError {
	return Errorf(ExitUsage, "usage", "", format, a...)
}

// Validation is a convenience for local validation errors (exit 7).
func Validation(format string, a ...any) *CLIError {
	return Errorf(ExitValidation, "validation", "", format, a...)
}

// CodeOf returns the exit code for any error (defaults to ExitError).
func CodeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CLIError
	if asCLIError(err, &ce) {
		return ce.Code
	}
	return ExitError
}
