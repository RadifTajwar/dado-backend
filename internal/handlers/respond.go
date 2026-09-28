package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const maxJSONBody = 1 << 20 // 1 MB

var okBody = map[string]bool{"ok": true}

// errorBody is the shape of every error response (see ApiErrorBody in the frontend).
type errorBody struct {
	Error  string      `json:"error"`
	Fields fieldErrors `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// writeInvalid answers 400 with one message per problem field.
func writeInvalid(w http.ResponseWriter, fields fieldErrors) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: "Please check the highlighted fields.", Fields: fields})
}

// writeServerError logs the real error and gives the client a generic one.
func writeServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong on our side. Please try again.")
}

// decodeJSON reads a JSON body (max 1 MB) into dst. Fields missing from the
// body keep whatever dst already held, which is how partial updates work.
// On failure it writes the error response and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusBadRequest, "Send the request body as JSON.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil {
		return true
	}

	var tooBig *http.MaxBytesError
	var wrongType *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "That request is too large.")
	case errors.As(err, &wrongType) && wrongType.Field != "":
		writeInvalid(w, fieldErrors{wrongType.Field: "This value has the wrong type."})
	default:
		writeError(w, http.StatusBadRequest, "The request body is not valid JSON.")
	}
	return false
}

// pathID reads the {id} in the URL; a malformed id is simply "not found".
func pathID(w http.ResponseWriter, r *http.Request) (bson.ObjectID, bool) {
	id, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Not found.")
		return id, false
	}
	return id, true
}
