package shared

import (
	"io"
	"net/http"
)

func ResponseWriter(w io.Writer, msg string, code int) {
	rw := w.(http.ResponseWriter)

	http.Error(rw, msg, code)
}
