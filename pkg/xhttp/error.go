package xhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// Err implements the error interface, wrapping the underlying error along with a status code and message safe for HTTP responses.
type Err struct {
	Code int
	Msg  string
	Err  error // underlying error
}

func (e *Err) Error() string {
	return fmt.Sprintf(`HTTP error: status: %d message: "%s" underlying: %s`, e.Code, e.Msg, e.Err)
}

func (e *Err) Unwrap() error { return e.Err }

func logError(ctx context.Context, err error) {
	// get logger from context
	logger := xlog.FromContext(ctx)
	if logger == nil { // fallback to console
		fmt.Println(err.Error())
		return
	}
	logger.Error(err.Error())
}

// Error logs the error and sends an http response. If the error is an [Err], it sends the given
// message and status code. Otherwise, it sends a generic "Internal server error" and 500 status code.
func Error(ctx context.Context, w http.ResponseWriter, err error) {
	logError(ctx, err)
	var e *Err
	if errors.As(err, &e) {
		http.Error(w, e.Msg, e.Code)
	} else {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
