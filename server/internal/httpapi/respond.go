package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	writeJSON(w, status, map[string]any{"error": apiError{Code: code, Message: msg, Details: details}})
}

// fail maps service and index errors onto HTTP status codes.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, index.ErrNotFound), errors.Is(err, vault.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, service.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, service.ErrTaskGone):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, service.ErrAlreadyExists()):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, vault.ErrOutside):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
	default:
		writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error(), nil)
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// pathID reads a wildcard path segment, undoing URL escaping so that note
// paths like "20-projects%2Flumen.md" work.
func pathID(r *http.Request, name string) string {
	raw := r.PathValue(name)
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

func intParam(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func csvParam(r *http.Request, name string) []string {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dateParam(r *http.Request, name string) domain.Date {
	v := r.URL.Query().Get(name)
	if v == "" {
		return domain.Date{}
	}
	if d, ok := domain.Relative(v, time.Now()); ok {
		return d
	}
	d, err := domain.ParseDate(v)
	if err != nil {
		return domain.Date{}
	}
	return d
}

// sortParam parses "due,-priority" into sort keys.
func sortParam(r *http.Request) []domain.SortKey {
	var out []domain.SortKey
	for _, f := range csvParam(r, "sort") {
		k := domain.SortKey{Field: strings.TrimPrefix(f, "-"), Desc: strings.HasPrefix(f, "-")}
		out = append(out, k)
	}
	return out
}

type listEnvelope[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
