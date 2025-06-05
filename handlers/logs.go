package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// HandleLogsPage is the HTTP handler for the /logs endpoint.
// It serves an HTML page displaying recent server logs.
// If the request is an HTMX request (HX-Request: true header), it serves only the log entries fragment.
// Log entries related to serving the logs page itself are filtered out from the display.
func (ah *AppHandlers) HandleLogsPage(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetReqID(r.Context())
	localLogger := ah.Logger.With("request_id", requestID)
	// The log message "Serving logs page" (and its HTMX variant) will be filtered out before display.
	localLogger.Info("Serving logs page")

	if ah.LogsTemplate == nil {
		http.Error(
			w,
			"Logs template not loaded. Check server logs for details.",
			http.StatusInternalServerError,
		)
		localLogger.Error("Logs template is nil, cannot render page.")
		return
	}

	rawLogs := ah.RingBufferLogger.GetLogs()
	filteredLogs := make([]string, 0, len(rawLogs))
	for _, logStr := range rawLogs {
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(logStr), &logEntry); err == nil {
			if msg, ok := logEntry["msg"].(string); ok {
				// Filter out messages related to serving the logs page itself and its content
				if msg == "Serving logs page" || msg == "Serving logs page content (HTMX request)" {
					continue // Skip this log entry
				}
			}
		}
		filteredLogs = append(filteredLogs, logStr)
	}

	data := struct {
		Logs []string
	}{
		Logs: filteredLogs,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var err error
	// Check for HTMX request header
	if r.Header.Get("HX-Request") == "true" {
		localLogger.Info("Serving logs page content (HTMX request)")
		// Render only the "logentries" block for HTMX requests
		err = ah.LogsTemplate.ExecuteTemplate(w, "logentries", data)
	} else {
		// Render the full page for normal browser requests
		err = ah.LogsTemplate.Execute(w, data)
	}

	if err != nil {
		localLogger.Error(
			"Failed to execute logs template",
			"error",
			err,
			"is_htmx",
			r.Header.Get("HX-Request") == "true",
		)
		http.Error(w, "Failed to render logs page", http.StatusInternalServerError)
	}
}
