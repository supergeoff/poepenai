package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog" // Using slog for structured logging
	"net/http"
	"strings"
	"time"

	"github.com/supergeoff/poepenai/types"
)

const (
	// poeAPIBaseURL is the base URL for the Poe Bot Query API.
	poeAPIBaseURL = "https://api.poe.com/bot/"
	// defaultTimeout is the default HTTP client timeout for non-streaming operations.
	defaultTimeout = 120 * time.Second
	// sseReadTimeout is the HTTP client timeout used for SSE streaming requests,
	// allowing for long-lived connections.
	sseReadTimeout = 5 * time.Minute
	// maxRetries is the number of times to retry a query on failure.
	maxRetries = 2
	// retrySleepTime is the base duration to wait before retrying.
	retrySleepTime = 500 * time.Millisecond
)

// PoeClient facilitates communication with Poe bots via the Poe Bot Query API.
// It handles making HTTP requests and processing Server-Sent Event (SSE) streams.
type PoeClient struct {
	// HTTPClient is the underlying HTTP client used for making requests.
	HTTPClient *http.Client
}

// NewPoeClient creates and returns a new PoeClient with a default HTTP client configuration.
func NewPoeClient() *PoeClient {
	return &PoeClient{
		HTTPClient: &http.Client{
			Timeout: defaultTimeout, // Default timeout for general requests. SSE uses sseReadTimeout.
		},
	}
}

// StreamQuery sends a query to the specified Poe bot and streams the response as Server-Sent Events.
// It handles retries on failure.
//
// Parameters:
//   - ctx: The context for the request, allowing for cancellation.
//   - botName: The name of the Poe bot to query.
//   - request: The PoeQueryRequest containing the query details. The APIKey field within this request
//     struct is part of the JSON payload sent to Poe and should be set by the caller/mapper.
//   - apiKey: The Poe Platform API Key used for the Authorization Bearer token in the HTTP request.
//
// Returns:
//   - A read-only channel for receiving PoeSSEEvent objects.
//   - A read-only channel for receiving an error if the query ultimately fails after retries.
func (c *PoeClient) StreamQuery(
	ctx context.Context,
	botName string,
	request *types.PoeQueryRequest,
	apiKey string,
) (<-chan types.PoeSSEEvent, <-chan error) {
	eventChan := make(chan types.PoeSSEEvent)
	errChan := make(chan error, 1) // Buffered error channel

	go func() {
		defer close(eventChan)
		defer close(errChan)

		var lastErr error
		for i := 0; i < maxRetries+1; i++ {
			if i > 0 {
				slog.Info(
					"Retrying query to Poe bot",
					"bot_name", botName,
					"attempt", i+1,
					"max_attempts", maxRetries+1,
				)
				time.Sleep(retrySleepTime * time.Duration(i)) // Exponential backoff could be better
			}

			// Pass apiKey to performStreamQuery
			err := c.performStreamQuery(ctx, botName, request, apiKey, eventChan)
			if err == nil {
				return // Success
			}

			lastErr = err
			slog.Error(
				"Error during stream query attempt",
				"bot_name", botName,
				"attempt", i+1,
				"error", err,
			)

			// Check if context is cancelled before retrying
			select {
			case <-ctx.Done():
				errChan <- fmt.Errorf("context cancelled during retry: %w", ctx.Err())
				return
			default:
			}

			// TODO: Implement more sophisticated retry logic (e.g., based on error type)
			// For now, retrying on any error from performStreamQuery.
		}
		errChan <- fmt.Errorf("failed to query bot %s after %d retries: %w", botName, maxRetries, lastErr)
	}()

	return eventChan, errChan
}

// dispatchPoeEvent handles logging and sending a PoeSSEEvent.
// It specifically parses and logs "error" type events with more detail.
func dispatchPoeEvent(
	ctx context.Context,
	event types.PoeSSEEvent,
	botName string,
	eventChan chan<- types.PoeSSEEvent,
) error {
	if event.Event == "error" {
		var poeErrData types.PoeErrorEventData
		if err := json.Unmarshal([]byte(event.Data), &poeErrData); err != nil {
			slog.Error("Failed to unmarshal Poe error event data",
				"raw_data", event.Data,
				"parse_error", err.Error(),
				"bot_name", botName,
			)
			// Still send the raw event if parsing fails, caller might handle raw data
		} else {
			logLevel := slog.LevelWarn // Default to Warn for API-level issues from Poe

			logAttrs := []slog.Attr{
				slog.String("bot_name", botName),
				slog.Bool("poe_allow_retry", poeErrData.AllowRetry),
			}
			if poeErrData.Text != nil {
				logAttrs = append(logAttrs, slog.String("poe_error_text", *poeErrData.Text))
			}
			if poeErrData.ErrorType != nil {
				logAttrs = append(logAttrs, slog.String("poe_error_type", *poeErrData.ErrorType))
			}

			slog.Default().LogAttrs(ctx, logLevel, "Poe API returned an error event", logAttrs...)
		}
	} else {
		// For non-error events, log event type and data length at debug level
		slog.Debug("Dispatching Poe SSE event",
			"event_type", event.Event,
			"data_length", len(event.Data), // Log length instead of full data for brevity
			"bot_name", botName,
		)
	}

	// Send the event to the channel
	select {
	case eventChan <- event:
		return nil
	case <-ctx.Done():
		slog.Warn(
			"Context cancelled while sending event to channel",
			"event_type",
			event.Event,
			"error",
			ctx.Err(),
		)
		return fmt.Errorf(
			"context cancelled while sending event type %s: %w",
			event.Event,
			ctx.Err(),
		)
	}
}

func (c *PoeClient) performStreamQuery(
	ctx context.Context,
	botName string,
	request *types.PoeQueryRequest, // request.APIKey should be set by mapper
	apiKey string, // This is the key for the Authorization header
	eventChan chan<- types.PoeSSEEvent,
) error {
	// The request.APIKey field within the PoeQueryRequest struct is part of the
	// JSON payload sent to Poe. It's distinct from the Authorization header API key.
	// The mapper should have already set request.APIKey if needed by the Poe bot/platform.
	// Here, we use the passed 'apiKey' for the Authorization Bearer token.

	jsonData, err := json.Marshal(request)
	if err != nil {
		slog.Error("Failed to marshal Poe query request for client", "error", err)
		return fmt.Errorf("failed to marshal Poe query request: %w", err)
	}
	slog.Debug("Poe request JSON body to be sent", "body", string(jsonData))

	url := poeAPIBaseURL + botName
	slog.Debug("Poe API request URL", "url", url)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		slog.Error("Failed to create HTTP request for Poe", "error", err)
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// Redact API key for logging
	redactedAPIKeyHeader := "Bearer sk-...key"
	if len(apiKey) > 8 {
		redactedAPIKeyHeader = "Bearer " + apiKey[:5] + "..." + apiKey[len(apiKey)-4:]
	} else if apiKey != "" {
		redactedAPIKeyHeader = "Bearer sk-..." // very short key
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	slog.Debug(
		"Sending HTTP request to Poe",
		"method",
		"POST",
		"url",
		url,
		"headers",
		map[string][]string{
			"Content-Type":  {"application/json"},
			"Accept":        {"text/event-stream"},
			"Authorization": {redactedAPIKeyHeader},
		},
	)

	// Custom client for SSE to handle longer timeouts during read
	sseClient := &http.Client{
		Timeout: sseReadTimeout,
	}

	resp, err := sseClient.Do(req)
	if err != nil {
		slog.Error("Failed to execute HTTP request to Poe", "error", err)
		return fmt.Errorf("failed to execute HTTP request to Poe: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close response body", "error", err)
		}
	}()

	slog.Debug(
		"Received HTTP response from Poe",
		"status_code",
		resp.StatusCode,
		"status_text",
		resp.Status,
	)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			slog.Error(
				"Failed to read error response body from Poe",
				"original_status",
				resp.StatusCode,
				"read_error",
				readErr,
			)
			return fmt.Errorf(
				"poe API request failed with status %d and could not read error body: %w",
				resp.StatusCode,
				readErr,
			)
		}
		slog.Error(
			"Poe API request failed",
			"status_code",
			resp.StatusCode,
			"response_body",
			string(bodyBytes),
		)
		return fmt.Errorf(
			"poe API request failed with status %d: %s",
			resp.StatusCode,
			string(bodyBytes),
		)
	}

	reader := bufio.NewReader(resp.Body)
	var currentEvent types.PoeSSEEvent
	var accumulatedData strings.Builder // To handle multi-line data fields

	for {
		lineBytes, err := reader.ReadBytes('\n')
		line := string(lineBytes)

		if err != nil {
			if err == io.EOF {
				// Process any accumulated data before returning EOF
				if accumulatedData.Len() > 0 ||
					currentEvent.Event != "" { // Ensure event has type if data is empty
					currentEvent.Data = strings.TrimSpace(accumulatedData.String())
					if dispatchErr := dispatchPoeEvent(ctx, currentEvent, botName, eventChan); dispatchErr != nil {
						// dispatchPoeEvent already logs context cancellation
						return dispatchErr
					}
				}
				slog.Debug("EOF reached while reading SSE stream from Poe.")
				return nil // End of stream
			}
			// Check if context was cancelled
			select {
			case <-ctx.Done():
				slog.Warn(
					"Context cancelled while reading stream from Poe",
					"underlying_error",
					err,
				)
				return fmt.Errorf("context cancelled while reading stream: %w", ctx.Err())
			default:
				slog.Error("Error reading SSE stream from Poe", "error", err)
				return fmt.Errorf("error reading SSE stream from Poe: %w", err)
			}
		}

		trimmedLine := strings.TrimSpace(line)
		slog.Debug("Raw SSE line from Poe", "line", trimmedLine)

		if strings.HasPrefix(line, "event:") {
			// If there's accumulated data for a previous event, dispatch it first
			if accumulatedData.Len() > 0 ||
				currentEvent.Event != "" { // Ensure event has type if data is empty
				currentEvent.Data = strings.TrimSpace(accumulatedData.String())
				// Dispatch only if event type is set (it might be from a previous line)
				if currentEvent.Event != "" {
					if dispatchErr := dispatchPoeEvent(ctx, currentEvent, botName, eventChan); dispatchErr != nil {
						return dispatchErr
					}
				}
				accumulatedData.Reset()
			}
			currentEvent = types.PoeSSEEvent{} // Reset for new event
			currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			// Append data, removing the "data:" prefix. SSE spec allows multi-line data.
			accumulatedData.WriteString(strings.TrimPrefix(line, "data:"))
			// Data might continue on next line, so don't trim trailing space from accumulatedData yet.
		} else if trimmedLine == "" { // Empty line signifies end of an event
			if currentEvent.Event != "" || accumulatedData.Len() > 0 { // Ensure there's something to send
				currentEvent.Data = strings.TrimSpace(accumulatedData.String())
				if dispatchErr := dispatchPoeEvent(ctx, currentEvent, botName, eventChan); dispatchErr != nil {
					return dispatchErr
				}
				currentEvent = types.PoeSSEEvent{} // Reset for next event
				accumulatedData.Reset()
			}
		}
		// Ignore other lines (comments like ":this is a comment", or lines with only ID)
	}
}
