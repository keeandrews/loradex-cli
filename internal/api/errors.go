package api

import (
	"fmt"

	"github.com/keeandrews/loradex-cli/internal/output"
)

// apiErrorBody is the server's error envelope.
type apiErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
}

// toCLIError maps an HTTP status + parsed body to a CLIError with the right exit code.
func toCLIError(status int, body apiErrorBody) *output.CLIError {
	code := body.Error.Code
	msg := body.Error.Message
	hint := body.Error.Hint
	if msg == "" {
		msg = fmt.Sprintf("server returned %d", status)
	}

	exit := output.ExitError
	switch status {
	case 401:
		exit = output.ExitUnauth
		if hint == "" {
			hint = "run `loradex login`"
		}
	case 403:
		exit = output.ExitForbidden
	case 404:
		exit = output.ExitNotFound
	case 409:
		exit = output.ExitConflict
	case 413:
		exit = output.ExitQuota
	case 400, 422:
		exit = output.ExitValidation
	}
	if code == "" {
		code = defaultCodeName(status)
	}
	return &output.CLIError{Code: exit, CodeName: code, Message: msg, Hint: hint}
}

func defaultCodeName(status int) string {
	switch status {
	case 401:
		return "unauthenticated"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 413:
		return "payload_too_large"
	case 400, 422:
		return "invalid_request"
	default:
		return "server_error"
	}
}

// networkError wraps a transport failure as exit-code 10.
func networkError(err error) *output.CLIError {
	return &output.CLIError{
		Code: output.ExitNetwork, CodeName: "network_error",
		Message: "could not reach the API: " + err.Error(),
		Hint:    "check your connection and --endpoint", Err: err,
	}
}
