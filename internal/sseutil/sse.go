package sseutil

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SetHeaders sets the standard SSE headers
func SetHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// Flush flushes the response writer if it supports it
func Flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// WriteEvent writes an SSE event with data in JSON
func WriteEvent(w http.ResponseWriter, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
	return err
}

// WriteEventString writes an SSE event with data as a string
func WriteEventString(w http.ResponseWriter, eventData string) error {
	_, err := fmt.Fprintf(w, "data: %s\n\n", eventData)
	return err
}
