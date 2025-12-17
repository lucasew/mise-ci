package httputil

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(data)
}

// WriteText writes a plain text response
func WriteText(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(message))
}

// WriteError writes a formatted error response
func WriteError(w http.ResponseWriter, status int, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	WriteText(w, status, message)
}

// WriteErrorMessage writes a simple error message
func WriteErrorMessage(w http.ResponseWriter, status int, message string) {
	WriteText(w, status, message)
}
