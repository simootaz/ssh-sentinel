package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

const maxBodyBytes = 1 << 20 // 1 MiB, far above any payload of the contract

// writeJSON sends v as the body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, model.ErrorResponse{Error: msg})
}

// decodeJSON reads the body into v. Unknown fields are ignored, as the
// contract requires for additive changes. It answers 400 itself and returns
// false when the body is not valid JSON.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return false
	}
	if len(body) > maxBodyBytes {
		writeError(w, http.StatusBadRequest, "body too large")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID reports whether s looks like a UUID. Checked before any query so
// that a malformed id never reaches PostgreSQL's uuid parser.
func isUUID(s string) bool { return uuidRe.MatchString(s) }

// pathID returns the {id} path value, lower-cased. A malformed id answers
// 404 itself: an id that cannot exist is an unknown id.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.ToLower(r.PathValue("id"))
	if !isUUID(id) {
		writeError(w, http.StatusNotFound, "not found")
		return "", false
	}
	return id, true
}

// jsonErrorWriter turns the plain-text 404 and 405 that ServeMux writes on
// its own into the contract's {"error": ...} bodies. Our handlers always set
// Content-Type application/json before writing, so a text/plain error can
// only come from the mux. It also records the status for the access log.
type jsonErrorWriter struct {
	http.ResponseWriter
	status  int
	wrote   bool
	swallow bool
}

func (w *jsonErrorWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	ct := w.Header().Get("Content-Type")
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) && strings.HasPrefix(ct, "text/plain") {
		w.swallow = true
		w.Header().Set("Content-Type", "application/json")
		w.Header().Del("X-Content-Type-Options")
		w.ResponseWriter.WriteHeader(status)
		msg := "not found"
		if status == http.StatusMethodNotAllowed {
			msg = "method not allowed"
		}
		_ = json.NewEncoder(w.ResponseWriter).Encode(model.ErrorResponse{Error: msg})
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *jsonErrorWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.swallow {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// Flush lets the long-polling route flush headers when the server supports it.
func (w *jsonErrorWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// errNotFound reports whether err is the store's not-found error.
func errNotFound(err error) bool { return errors.Is(err, db.ErrNotFound) }
