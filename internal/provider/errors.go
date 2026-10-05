package provider

import (
	"errors"
	"net/http"

	"google.golang.org/api/googleapi"
)

// isNotFound reports whether err is a Google API error with HTTP 404, as
// errors.As unwraps wrapped errors that a direct type check would miss.
func isNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}
