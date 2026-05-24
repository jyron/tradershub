package apiv1

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

func transientDBError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "SQLITE_LOCKED") {
		return huma.NewError(http.StatusServiceUnavailable, msg)
	}
	return nil
}
