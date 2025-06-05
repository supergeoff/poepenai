package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/supergeoff/poepenai/service"
	"github.com/supergeoff/poepenai/types"
)

// HandleChatCompletions is the HTTP handler for the OpenAI-compatible /v1/chat/completions endpoint.
// It processes incoming chat requests, transforms them for the Poe API, queries the specified Poe bot,
// and then transforms the Poe bot's response back into the OpenAI format.
// It supports both streaming and non-streaming responses.
// Authentication is expected via a Bearer token in the Authorization header, which is used as the Poe Platform API Key.
func (ah *AppHandlers) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	localLogger := ah.Logger.With("request_id", requestID)

	localLogger.Debug(
		"Received chat completion request",
		"method", r.Method,
		"url", r.URL.String(),
		"headers", r.Header,
	)

	var openAIReq types.OpenAIChatCompletionRequest

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		localLogger.Error("Failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			slog.Warn("Failed to close request body in chat handler", "error", err)
		}
	}()

	localLogger.Debug("Raw OpenAI request body", "body", string(bodyBytes))

	if err := json.Unmarshal(bodyBytes, &openAIReq); err != nil {
		localLogger.Error("Invalid request body JSON", "error", err)
		http.Error(w, fmt.Sprintf("Invalid request body JSON: %v", err), http.StatusBadRequest)
		return
	}

	localLogger.Debug("Decoded OpenAI request", "request_object", openAIReq)

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		localLogger.Warn("Missing Authorization header")
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		localLogger.Warn("Invalid Authorization header format")
		http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
		return
	}
	poePlatformAPIKey := parts[1]
	if poePlatformAPIKey == "" {
		localLogger.Warn("Empty Bearer token")
		http.Error(w, "Empty Bearer token", http.StatusUnauthorized)
		return
	}
	localLogger.Debug("Extracted parameters for Poe query",
		"bot_name", openAIReq.Model,
		"poe_api_key_present", poePlatformAPIKey != "",
	)

	messageID := service.GenerateID("msg")
	conversationID := "poepenai-default-conversation" // TODO: Make this configurable or dynamic

	localLogger.Info( // This can remain Info
		"Processing chat completion request",
		"model", openAIReq.Model,
		"stream", openAIReq.Stream,
	)

	poeQueryReq, err := service.TransformOpenAIRequestToPoeQuery(
		&openAIReq,
		poePlatformAPIKey,
		conversationID,
		messageID,
	)
	if err != nil {
		localLogger.Error("Error transforming OpenAI request to Poe query", "error", err)
		http.Error(
			w,
			fmt.Sprintf("Error transforming request: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	if poeQueryReqBytes, marshalErr := json.Marshal(poeQueryReq); marshalErr == nil {
		localLogger.Debug(
			"Transformed PoeQueryRequest for Poe API",
			"poe_request_body",
			string(poeQueryReqBytes),
		)
	} else {
		localLogger.Error("Failed to marshal PoeQueryRequest for logging", "error", marshalErr)
	}

	botName := openAIReq.Model
	localLogger.Debug("Calling Poe client StreamQuery", "bot_name", botName)

	if openAIReq.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			localLogger.Error("Streaming unsupported by the server")
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		eventChan, errChan := ah.PoeClient.StreamQuery(ctx, botName, poeQueryReq, poePlatformAPIKey)

		completionID := service.GenerateID("chatcmpl")
		createdTimestamp := time.Now().Unix()
		isFirstContentChunkEvent := true
		hasMadeToolCallEvent := false

		for {
			select {
			case <-ctx.Done():
				localLogger.Info("Client disconnected or context timed out during streaming")
				return
			case err, ok := <-errChan:
				if !ok { // errChan closed
					localLogger.Debug("Poe error channel closed.")
					return
				}
				if err != nil {
					localLogger.Error("Error from Poe stream", "error", err)
					// TODO: Send OpenAI error SSE if defined, then [DONE]
					return
				}
				localLogger.Debug(
					"Poe stream query finished without error from error channel (errChan sent nil).",
				)
				// If errChan sends nil, it means the goroutine in StreamQuery exited cleanly.
				// The eventChan loop should continue until eventChan is closed.
				// This path might not be strictly necessary if eventChan closure always handles termination.
				return
			case poeEvent, ok := <-eventChan:
				if !ok {
					localLogger.Info("Poe event channel closed by sender. Stream assumed complete.")
					doneChunk := []byte("data: [DONE]\n\n")
					if _, writeErr := w.Write(doneChunk); writeErr != nil {
						localLogger.Error(
							"Error writing final [DONE] to stream after channel close",
							"error",
							writeErr,
						)
					}
					flusher.Flush()
					return
				}
				localLogger.Debug(
					"Received Poe event from stream",
					"event_type",
					poeEvent.Event,
					"data",
					poeEvent.Data,
				)

				openAIChunkBytes, err := service.TransformPoeEventToOpenAIChatCompletionChunk(
					poeEvent,
					openAIReq.Model,
					completionID,
					createdTimestamp,
					&isFirstContentChunkEvent,
					&hasMadeToolCallEvent,
				)
				if err != nil {
					localLogger.Error(
						"Error transforming Poe event to OpenAI chunk",
						"poe_event_type", poeEvent.Event,
						"error", err,
					)
					if strings.HasPrefix(err.Error(), "terminate_stream_with_error:") {
						localLogger.Error(
							"Terminating stream due to mapper error signal from Poe bot error event",
							"detail",
							err.Error(),
						)
						return
					}
					continue
				}

				if openAIChunkBytes != nil {
					localLogger.Debug(
						"Sending OpenAI chunk to client",
						"chunk",
						string(openAIChunkBytes),
					)
					if _, err := w.Write(openAIChunkBytes); err != nil {
						localLogger.Error("Error writing chunk to stream", "error", err)
						return
					}
					flusher.Flush()
				}

				if poeEvent.Event == "done" {
					localLogger.Info("Poe 'done' event processed. Sending final [DONE] marker.")
					doneChunk := []byte("data: [DONE]\n\n")
					if _, writeErr := w.Write(doneChunk); writeErr != nil {
						localLogger.Error(
							"Error writing [DONE] to stream after Poe 'done' event",
							"error",
							writeErr,
						)
					}
					flusher.Flush()
					return
				}
			}
		}
	} else { // Non-streaming response
		var allPoeEvents []types.PoeSSEEvent
		eventChan, errChan := ah.PoeClient.StreamQuery(ctx, botName, poeQueryReq, poePlatformAPIKey)

	collectEventsLoop:
		for {
			select {
			case <-ctx.Done():
				localLogger.Warn("Request timed out or client disconnected during non-streaming response aggregation")
				http.Error(w, "Request timed out or client disconnected", http.StatusGatewayTimeout)
				return
			case err, ok := <-errChan:
				if !ok { // errChan closed
					localLogger.Debug("Poe error channel closed during non-streaming aggregation.")
					break collectEventsLoop
				}
				if err != nil {
					localLogger.Error("Error from Poe stream during non-streaming response aggregation", "error", err)
					http.Error(w, fmt.Sprintf("Error from Poe: %v", err), http.StatusInternalServerError)
					return
				}
				localLogger.Debug("Poe stream query finished (errChan sent nil) during non-streaming. Processing collected events.")
				break collectEventsLoop
			case event, ok := <-eventChan:
				if !ok {
					localLogger.Debug("Poe event channel closed during non-streaming. Processing collected events.")
					break collectEventsLoop
				}
				localLogger.Debug("Collected Poe event for non-streaming response", "event_type", event.Event, "data", event.Data)
				allPoeEvents = append(allPoeEvents, event)
			}
		}

		localLogger.Info("Aggregating non-streaming response", "collected_event_count", len(allPoeEvents))
		completionID := service.GenerateID("chatcmpl")
		createdTimestamp := time.Now().Unix()

		openAIResp, err := service.AggregatePoeEventsToOpenAIResponse(
			allPoeEvents,
			openAIReq.Model,
			completionID,
			createdTimestamp,
		)
		if err != nil {
			localLogger.Error("Error aggregating Poe response", "error", err)
			http.Error(w, fmt.Sprintf("Error aggregating Poe response: %v", err), http.StatusInternalServerError)
			return
		}

		if finalRespBytes, marshalErr := json.Marshal(openAIResp); marshalErr == nil {
			localLogger.Debug("Final aggregated OpenAI response (non-streaming)", "response_body", string(finalRespBytes))
		} else {
			localLogger.Error("Failed to marshal final OpenAI response for logging", "error", marshalErr)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(openAIResp); err != nil {
			localLogger.Error("Error encoding non-streaming response", "error", err)
		}
		localLogger.Info("Successfully sent non-streaming response")
	}
}
