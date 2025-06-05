package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/supergeoff/poepenai/types"
)

const (
	poeProtocolVersion  = "1.1"
	poeRequestTypeQuery = "query"
)

// OpenAIToPoeRole maps OpenAI message roles to their Poe protocol equivalents.
func OpenAIToPoeRole(openAIRole string) string {
	slog.Debug("Mapping OpenAI role to Poe role", "openai_role", openAIRole)
	switch openAIRole {
	case "system":
		return "system"
	case "user":
		return "user"
	case "developer": // OpenAI's new "developer" role can map to Poe's "user" or "system"
		slog.Debug("Mapping OpenAI 'developer' role to Poe 'user' role.")
		return "user"
	case "assistant":
		return "bot"
	case "tool":
		slog.Warn(
			"OpenAI 'tool' role does not directly map to Poe's ProtocolMessage role. Mapping to 'user'.",
			"openai_role",
			openAIRole,
		)
		return "user" // Poe protocol doesn't have a direct "tool" role for messages.
	default:
		slog.Warn("Unknown OpenAI role, defaulting to 'user' for Poe.", "openai_role", openAIRole)
		return "user"
	}
}

// OpenAIModelsToPoeToolDefinitions maps a slice of OpenAI tool definitions to Poe tool definitions.
// Currently, only "function" type tools are supported.
func OpenAIModelsToPoeToolDefinitions(openAITools []types.OpenAITool) []types.PoeToolDefinition {
	slog.Debug("Mapping OpenAI tools to Poe tool definitions", "num_openai_tools", len(openAITools))
	if openAITools == nil {
		return nil
	}
	poeTools := make([]types.PoeToolDefinition, 0, len(openAITools))
	for _, oaiTool := range openAITools {
		if oaiTool.Type != "function" {
			slog.Warn(
				"Unsupported OpenAI tool type, only 'function' is supported for Poe.",
				"tool_type", oaiTool.Type,
			)
			continue // Skip non-function tools
		}
		poeTools = append(poeTools, types.PoeToolDefinition{
			Type: "function",
			Function: types.PoeToolFunctionDefinition{
				Name:        oaiTool.Function.Name,
				Description: oaiTool.Function.Description,
				Parameters: types.PoeToolFunctionParamsDefinition{
					Type:       oaiTool.Function.Parameters.Type,
					Properties: oaiTool.Function.Parameters.Properties,
					Required:   oaiTool.Function.Parameters.Required,
				},
			},
		})
	}
	return poeTools
}

// TransformOpenAIRequestToPoeQuery converts an OpenAI chat completion request
// into a PoeQueryRequest suitable for sending to a Poe bot.
// It maps relevant fields and logs warnings for unsupported or partially supported features.
func TransformOpenAIRequestToPoeQuery(
	openAIReq *types.OpenAIChatCompletionRequest,
	poeAPIKey string, // The Poe Platform API Key for authentication.
	conversationID string, // Identifier for the conversation session.
	lastMessageID string, // Unique identifier for this specific query.
) (*types.PoeQueryRequest, error) {
	if openAIReq == nil {
		slog.Error("Attempted to transform nil OpenAI request to Poe query")
		return nil, fmt.Errorf("openAI request is nil") // Corrected capitalization
	}

	slog.Debug(
		"Transforming OpenAI request to Poe query",
		"model",
		openAIReq.Model,
		"num_messages",
		len(openAIReq.Messages),
	)

	poeMessages := make([]types.PoeProtocolMessage, len(openAIReq.Messages))
	for i, msg := range openAIReq.Messages {
		var contentStr string
		switch c := msg.Content.(type) {
		case string:
			contentStr = c
		case []types.OpenAIContentPart: // Handles already correctly typed content parts
			var textParts []string
			for _, part := range c {
				switch part.Type {
				case "text":
					textParts = append(textParts, part.Text)
				case "image_url":
					slog.Warn("Image_url content part received in OpenAI request (typed []OpenAIContentPart), mapping to Poe attachment not yet implemented.", "message_index", i)
				default:
					slog.Warn("Unknown OpenAI content part type (typed []OpenAIContentPart)", "type", part.Type, "message_index", i)
				}
			}
			contentStr = strings.Join(textParts, "\n")
		case []interface{}: // Handles content parts unmarshalled as generic slice
			var textParts []string
			slog.Debug("OpenAI message content is []interface{}, processing as content parts.", "message_index", i)
			for partIdx, item := range c {
				if partMap, ok := item.(map[string]interface{}); ok {
					partType, _ := partMap["type"].(string)
					switch partType {
					case "text":
						if textVal, ok := partMap["text"].(string); ok {
							textParts = append(textParts, textVal)
						} else {
							slog.Warn("OpenAI 'text' content part has non-string text field.", "message_index", i, "part_index", partIdx, "text_field", partMap["text"])
						}
					case "image_url":
						// imageURLData, _ := partMap["image_url"].(map[string]interface{})
						// url, _ := imageURLData["url"].(string)
						slog.Warn("Image_url content part received in OpenAI request (unmarshalled []interface{}), mapping to Poe attachment not yet implemented.", "message_index", i, "part_index", partIdx)
					default:
						slog.Warn("Unknown OpenAI content part type in []interface{}", "type", partType, "message_index", i, "part_index", partIdx)
					}
				} else {
					slog.Warn("Item in OpenAI message content array is not a valid content part object (map[string]interface{}).", "message_index", i, "part_index", partIdx, "item_type", fmt.Sprintf("%T", item))
				}
			}
			contentStr = strings.Join(textParts, "\n")
		default:
			slog.Warn("OpenAI message content is not a string, []OpenAIContentPart, or []interface{}, attempting to serialize.", "message_index", i, "content_type", fmt.Sprintf("%T", msg.Content))
			complexContentBytes, err := json.Marshal(msg.Content)
			if err == nil {
				contentStr = string(complexContentBytes)
			} else {
				contentStr = "[unsupported content type]"
			}
		}

		poeMessages[i] = types.PoeProtocolMessage{
			Role:    OpenAIToPoeRole(msg.Role),
			Content: contentStr,
		}
	}

	var poeStopSequences []string
	if openAIReq.Stop != nil {
		switch v := openAIReq.Stop.(type) {
		case string:
			if v != "" {
				poeStopSequences = []string{v}
			}
		case []interface{}:
			for _, sVal := range v {
				if str, ok := sVal.(string); ok {
					poeStopSequences = append(poeStopSequences, str)
				}
			}
		case []string:
			poeStopSequences = v
		}
	}

	var poeTemperature *float64
	if openAIReq.Temperature != 0 {
		temp := openAIReq.Temperature
		poeTemperature = &temp
	}

	var poeLogitBias map[string]float64
	if openAIReq.LogitBias != nil {
		poeLogitBias = make(map[string]float64)
		for tokenID, bias := range openAIReq.LogitBias {
			poeLogitBias[tokenID] = float64(bias)
		}
	}

	if openAIReq.N > 1 {
		slog.Warn(
			"OpenAI request N > 1, but Poe only supports N=1. Proceeding with N=1.",
			"requested_n", openAIReq.N,
		)
	}

	if openAIReq.MaxTokens != nil || openAIReq.MaxCompletionTokens != nil {
		slog.Warn(
			"OpenAI 'max_tokens' or 'max_completion_tokens' provided, but not directly supported by Poe QueryRequest. This limit will not be enforced by Poe.",
		)
	}

	poeTools := OpenAIModelsToPoeToolDefinitions(openAIReq.Tools)
	if openAIReq.ToolChoice != nil {
		switch tc := openAIReq.ToolChoice.(type) {
		case string:
			if tc == "none" {
				slog.Debug("OpenAI tool_choice is 'none', omitting tools for Poe request.")
				poeTools = nil
			} else if tc != "auto" && tc != "required" {
				slog.Warn("OpenAI tool_choice is not directly mappable to Poe. Poe will use 'auto' behavior if tools are provided.", "tool_choice", tc)
			}
		case map[string]interface{}:
			if fnChoice, ok := tc["function"].(map[string]interface{}); ok {
				if fnName, ok := fnChoice["name"].(string); ok {
					slog.Warn("OpenAI tool_choice to call specific function is not directly supported by Poe. All defined tools will be sent.", "function_name", fnName)
				}
			} else {
				slog.Warn("OpenAI tool_choice object structure not recognized as specific function choice.", "tool_choice_object", tc)
			}
		default:
			slog.Warn("Unknown type for OpenAI tool_choice.", "tool_choice_type", fmt.Sprintf("%T", openAIReq.ToolChoice))
		}
	}

	skipSystemPrompt := false
	if len(openAIReq.Messages) == 0 || OpenAIToPoeRole(openAIReq.Messages[0].Role) != "system" {
		slog.Debug(
			"First message is not system or no messages; setting skip_system_prompt=true for Poe.",
		)
		skipSystemPrompt = true
	}

	poeQuery := &types.PoeQueryRequest{
		Version:          poeProtocolVersion,
		Type:             poeRequestTypeQuery,
		Query:            poeMessages,
		UserID:           openAIReq.User,
		ConversationID:   conversationID,
		MessageID:        lastMessageID,
		APIKey:           poeAPIKey,
		Temperature:      poeTemperature,
		SkipSystemPrompt: skipSystemPrompt,
		StopSequences:    poeStopSequences,
		Tools:            poeTools,
		LogitBias:        poeLogitBias,
	}
	slog.Debug(
		"Successfully transformed OpenAI request to Poe query",
		"poe_query_message_id",
		poeQuery.MessageID,
	)
	return poeQuery, nil
}

// MapPoeRoleToOpenAIRole maps Poe message roles back to their OpenAI equivalents.
func MapPoeRoleToOpenAIRole(poeRole string) string {
	slog.Debug("Mapping Poe role to OpenAI role", "poe_role", poeRole)
	switch poeRole {
	case "system":
		return "system"
	case "user":
		return "user"
	case "bot":
		return "assistant"
	default:
		slog.Warn("Unknown Poe role, defaulting to 'assistant' for OpenAI.", "poe_role", poeRole)
		return "assistant"
	}
}

// TransformPoeEventToOpenAIChatCompletionChunk converts a single Server-Sent Event (SSE) from a Poe bot
// into an OpenAI-compatible chat completion chunk.
// It handles different Poe event types ("text", "json" for tool calls, "error", "done", etc.)
// and formats them according to the OpenAI streaming API specification.
//   - poeEvent: The event received from the Poe bot.
//   - requestModelID: The model ID specified in the original OpenAI request.
//   - completionID: A unique ID generated for this entire chat completion stream.
//   - createdTimestamp: The Unix timestamp when this completion stream was initiated.
//   - isFirstContentChunk: Pointer to a boolean flag, managed by the caller, to indicate if this is the first
//     content-producing delta, used to set the 'role' field in the OpenAI chunk.
//   - hasMadeToolCall: Pointer to a boolean flag, managed by the caller, to track if any tool calls
//     have been processed, used to determine the 'finish_reason'.
//
// Returns the formatted SSE data as bytes, or an error.
func TransformPoeEventToOpenAIChatCompletionChunk(
	poeEvent types.PoeSSEEvent,
	requestModelID string,
	completionID string,
	createdTimestamp int64,
	isFirstContentChunk *bool,
	hasMadeToolCall *bool,
) ([]byte, error) {
	slog.Debug(
		"Transforming Poe event to OpenAI chunk",
		"poe_event_type",
		poeEvent.Event,
		"poe_event_data_len",
		len(poeEvent.Data),
	)
	var choice types.OpenAIStreamChoice
	var usage *types.OpenAIUsage // OpenAI 'usage' is not typically provided by Poe stream, so this remains nil.
	roleWasSetThisChunk := false

	switch poeEvent.Event {
	case "text":
		var poeRespData types.PoePartialResponseData
		if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
			slog.Error(
				"Error unmarshalling Poe 'text' event data",
				"error", err, "data", poeEvent.Data,
			)
			return nil, fmt.Errorf("error unmarshalling Poe 'text' event data: %w", err)
		}
		choice.Delta.Content = poeRespData.Text
		if *isFirstContentChunk {
			choice.Delta.Role = "assistant"
			roleWasSetThisChunk = true
		}
	case "replace_response": // Poe-specific, treat like text for now
		var poeRespData types.PoePartialResponseData
		if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
			slog.Error(
				"Error unmarshalling Poe 'replace_response' event data",
				"error", err, "data", poeEvent.Data,
			)
			return nil, fmt.Errorf("error unmarshalling Poe 'replace_response' event data: %w", err)
		}
		choice.Delta.Content = poeRespData.Text // Replace implies full content
		if *isFirstContentChunk {
			choice.Delta.Role = "assistant"
			roleWasSetThisChunk = true
		}
		slog.Debug("Processed 'replace_response' as text delta for OpenAI chunk.")
	case "suggested_reply": // OpenAI doesn't have a direct equivalent in stream chunks
		slog.Debug("Ignoring Poe 'suggested_reply' event for OpenAI stream.")
		return nil, nil // Skip this event
	case "meta": // Poe-specific metadata, not directly mappable to OpenAI chunk content
		slog.Debug("Ignoring Poe 'meta' event for OpenAI stream.", "data", poeEvent.Data)
		return nil, nil // Skip this event
	case "json": // Often used by Poe for tool calls or structured data, mimicking OpenAI
		var poeRespData types.PoePartialResponseData
		if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
			slog.Error(
				"Error unmarshalling Poe 'json' event data string to PoePartialResponseData",
				"error", err, "data", poeEvent.Data,
			)
			return nil, fmt.Errorf(
				"error unmarshalling Poe 'json' event data string to PoePartialResponseData: %w",
				err,
			)
		}
		slog.Debug("Processing Poe 'json' event", "parsed_data", poeRespData.Data)

		if choicesList, ok := poeRespData.Data["choices"].([]interface{}); ok &&
			len(choicesList) > 0 {
			if choiceMap, ok := choicesList[0].(map[string]interface{}); ok {
				if deltaMap, ok := choiceMap["delta"].(map[string]interface{}); ok {
					if tcData, ok := deltaMap["tool_calls"].([]interface{}); ok {
						toolCalls, err := mapPoeToolCallDataToOpenAI(tcData)
						if err != nil {
							slog.Error(
								"Error in mapPoeToolCallDataToOpenAI from 'json' event",
								"error",
								err,
							)
						} else if len(toolCalls) > 0 {
							choice.Delta.ToolCalls = toolCalls
							*hasMadeToolCall = true
							slog.Debug("Mapped tool calls from Poe 'json' event", "num_tool_calls", len(toolCalls))
						}
					}
					if content, ok := deltaMap["content"].(string); ok && content != "" {
						choice.Delta.Content = content
						slog.Debug(
							"Extracted content from Poe 'json' event delta",
							"content_length",
							len(content),
						)
					}
				}
			}
		} else {
			slog.Warn("Poe 'json' event data does not match expected OpenAI chunk structure for tool_calls", "data", poeRespData.Data)
		}

		if *isFirstContentChunk && (choice.Delta.ToolCalls != nil || choice.Delta.Content != "") {
			choice.Delta.Role = "assistant"
			roleWasSetThisChunk = true
		}
	case "error":
		var poeErrData types.PoeErrorEventData
		if err := json.Unmarshal([]byte(poeEvent.Data), &poeErrData); err != nil {
			slog.Error(
				"Error unmarshalling Poe 'error' event data",
				"error", err, "data", poeEvent.Data,
			)
			return nil, fmt.Errorf(
				"terminate_stream_with_error: Poe bot reported an unparsable error: %s",
				poeEvent.Data,
			)
		}
		slog.Error("Received error event from Poe bot", "error_data", poeErrData)
		errMsg := "Poe bot reported an error."
		if poeErrData.Text != nil && *poeErrData.Text != "" {
			errMsg = *poeErrData.Text
		}
		return nil, fmt.Errorf("terminate_stream_with_error: %s", errMsg)

	case "done":
		slog.Debug("Processing Poe 'done' event.")
		finishReasonStr := "stop"
		if *hasMadeToolCall {
			finishReasonStr = "tool_calls"
		}
		choice.FinishReason = &finishReasonStr
		if *isFirstContentChunk && choice.Delta.Content == "" && choice.Delta.ToolCalls == nil {
			choice.Delta.Role = "assistant"
			roleWasSetThisChunk = true
		}
	default:
		slog.Warn(
			"Unhandled Poe event type, skipping for OpenAI chunk.",
			"event_type",
			poeEvent.Event,
			"data",
			poeEvent.Data,
		)
		return nil, nil
	}

	if choice.Delta.Content == "" && choice.Delta.ToolCalls == nil && choice.FinishReason == nil &&
		!roleWasSetThisChunk {
		slog.Debug(
			"No actual data in delta for OpenAI chunk, skipping send for this event.",
			"poe_event_type",
			poeEvent.Event,
		)
		return nil, nil
	}

	chunk := types.OpenAIChatCompletionChunk{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: createdTimestamp,
		Model:   requestModelID,
		Choices: []types.OpenAIStreamChoice{choice},
		Usage:   usage,
	}

	if roleWasSetThisChunk {
		*isFirstContentChunk = false
	}

	jsonData, err := json.Marshal(chunk)
	if err != nil {
		slog.Error("Error marshalling OpenAI chunk", "error", err)
		return nil, fmt.Errorf("error marshalling OpenAI chunk: %w", err)
	}

	var buffer bytes.Buffer
	buffer.WriteString("data: ")
	buffer.Write(jsonData)
	buffer.WriteString("\n\n")

	slog.Debug(
		"Successfully transformed Poe event to OpenAI chunk",
		"chunk_length",
		len(buffer.Bytes()),
	)
	return buffer.Bytes(), nil
}

// mapPoeToolCallDataToOpenAI attempts to parse tool call data from a Poe "json" event.
// Poe bots (especially those wrapping OpenAI models) might send tool calls in a structure
// similar to OpenAI's own streaming chunks, nested within the "json" event's data field.
// This function expects `poeToolCallsData` to be the `tool_calls` array from such a structure.
func mapPoeToolCallDataToOpenAI(poeToolCallsData []interface{}) ([]types.OpenAIToolCall, error) {
	slog.Debug(
		"Mapping Poe tool call data to OpenAI tool calls",
		"num_poe_tool_calls_data",
		len(poeToolCallsData),
	)
	var openAIToolCalls []types.OpenAIToolCall
	for i, item := range poeToolCallsData {
		tcMap, ok := item.(map[string]interface{})
		if !ok {
			slog.Error("Poe tool_call item is not a map", "item_index", i, "item", item)
			return nil, fmt.Errorf("poe tool_call item at index %d is not a map", i)
		}

		var oaiTC types.OpenAIToolCall

		if idVal, ok := tcMap["id"]; ok {
			if id, idOk := idVal.(string); idOk {
				oaiTC.ID = id
			} else {
				slog.Warn("Poe tool_call item 'id' is not a string", "item_index", i, "id_value", idVal)
			}
		}
		if typeVal, ok := tcMap["type"]; ok {
			if typ, typeOk := typeVal.(string); typeOk {
				oaiTC.Type = typ
			} else {
				slog.Warn("Poe tool_call item 'type' is not a string", "item_index", i, "type_value", typeVal)
			}
		}

		if fnMapVal, ok := tcMap["function"]; ok {
			if fnMap, fnMapOk := fnMapVal.(map[string]interface{}); fnMapOk {
				if nameVal, ok := fnMap["name"]; ok {
					if name, nameOk := nameVal.(string); nameOk {
						oaiTC.Function.Name = name
					} else {
						slog.Warn("Poe tool_call function 'name' is not a string", "item_index", i, "name_value", nameVal)
					}
				}
				if argsVal, ok := fnMap["arguments"]; ok {
					if args, argsOk := argsVal.(string); argsOk {
						oaiTC.Function.Arguments = args
					} else {
						slog.Warn("Poe tool_call function 'arguments' is not a string", "item_index", i, "args_value", argsVal)
					}
				}
			} else {
				slog.Warn("Poe tool_call item 'function' is not a map", "item_index", i, "function_value", fnMapVal)
			}
		}
		openAIToolCalls = append(openAIToolCalls, oaiTC)
	}
	return openAIToolCalls, nil
}

// AggregatePoeEventsToOpenAIResponse processes a complete list of Poe SSE events
// and aggregates them into a single, non-streaming OpenAI chat completion response.
func AggregatePoeEventsToOpenAIResponse(
	poeEvents []types.PoeSSEEvent,
	requestModelID string,
	completionID string,
	createdTimestamp int64,
) (*types.OpenAIChatCompletionResponse, error) {
	slog.Debug(
		"Aggregating Poe events to OpenAI non-streaming response",
		"num_events",
		len(poeEvents),
	)
	var responseContent strings.Builder
	finalRole := "assistant" // Default role for the aggregated message.
	finalFinishReason := "stop"
	var openAIToolCalls []types.OpenAIToolCall
	var poeReportedErrorText string

	for _, poeEvent := range poeEvents {
		slog.Debug(
			"Aggregating event",
			"event_type",
			poeEvent.Event,
			"data_len",
			len(poeEvent.Data),
		)
		switch poeEvent.Event {
		case "text":
			var poeRespData types.PoePartialResponseData
			if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
				slog.Error(
					"Error unmarshalling Poe 'text' event data during aggregation",
					"error", err, "data", poeEvent.Data,
				)
				continue
			}
			responseContent.WriteString(poeRespData.Text)
		case "replace_response":
			var poeRespData types.PoePartialResponseData
			if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
				slog.Error(
					"Error unmarshalling Poe 'replace_response' event data during aggregation",
					"error", err, "data", poeEvent.Data,
				)
				continue
			}
			responseContent.Reset() // Clear previous content
			responseContent.WriteString(poeRespData.Text)
		case "json": // Could contain tool calls or other structured data
			var poeRespData types.PoePartialResponseData
			if err := json.Unmarshal([]byte(poeEvent.Data), &poeRespData); err != nil {
				slog.Error(
					"Error unmarshalling Poe 'json' event data during aggregation",
					"error", err, "data", poeEvent.Data,
				)
				continue
			}
			// Check for OpenAI-like tool call structure
			if choicesList, ok := poeRespData.Data["choices"].([]interface{}); ok &&
				len(choicesList) > 0 {
				if choiceMap, ok := choicesList[0].(map[string]interface{}); ok {
					if deltaMap, ok := choiceMap["delta"].(map[string]interface{}); ok {
						if tcData, ok := deltaMap["tool_calls"].([]interface{}); ok {
							mappedTcs, err := mapPoeToolCallDataToOpenAI(tcData)
							if err == nil {
								openAIToolCalls = append(openAIToolCalls, mappedTcs...)
							} else {
								slog.Error("Error mapping tool calls during aggregation from 'json' event's choices", "error", err)
							}
						}
						// Append content if present in the delta
						if content, ok := deltaMap["content"].(string); ok && content != "" {
							responseContent.WriteString(content)
						}
					}
				}
			} else if tcData, ok := poeRespData.Data["tool_calls"].([]interface{}); ok {
				// Some Poe bots might send tool_calls directly in the data field of a "json" event
				mappedTcs, err := mapPoeToolCallDataToOpenAI(tcData)
				if err == nil {
					openAIToolCalls = append(openAIToolCalls, mappedTcs...)
				} else {
					slog.Error("Error mapping tool calls directly from poeRespData.Data during aggregation", "error", err)
				}
			} else {
				// If not tool calls, it might be other JSON content.
				// For simplicity, we might try to append its string representation or log it.
				// Current OpenAI non-streaming response expects content as a single string.
				slog.Warn("Unhandled 'json' event structure during aggregation, attempting to stringify.", "data", poeRespData.Data)
				jsonBytes, err := json.Marshal(poeRespData.Data)
				if err == nil {
					responseContent.WriteString(string(jsonBytes))
				}
			}

		case "done":
			if len(openAIToolCalls) > 0 {
				finalFinishReason = "tool_calls"
			} else {
				finalFinishReason = "stop"
			}
		case "error":
			var poeErrData types.PoeErrorEventData
			if err := json.Unmarshal([]byte(poeEvent.Data), &poeErrData); err == nil &&
				poeErrData.Text != nil {
				poeReportedErrorText = *poeErrData.Text
				// Append error to content for non-streaming, as there's no separate error field in OpenAIResponseMessage
				responseContent.WriteString(fmt.Sprintf("\n[POE BOT ERROR]: %s", *poeErrData.Text))
			} else {
				poeReportedErrorText = "An unspecified error occurred from Poe bot."
				responseContent.WriteString(fmt.Sprintf("\n[POE BOT ERROR]: %s", poeReportedErrorText))
			}
			finalFinishReason = "stop" // Error usually means stop.
			slog.Error(
				"Poe bot reported an error during event aggregation",
				"error_text", poeReportedErrorText,
			)
			// Note: The OpenAI spec doesn't have a top-level error field in ChatCompletionResponse for bot errors.
			// Errors are typically HTTP status codes or in the stream. We include it in content here.

		case "meta", "suggested_reply": // Ignore these for aggregated response
			slog.Debug("Ignoring Poe event during aggregation", "event_type", poeEvent.Event)
		default:
			slog.Warn(
				"Unhandled Poe event type during aggregation",
				"event_type", poeEvent.Event, "data", poeEvent.Data,
			)
		}
	}

	contentStr := responseContent.String()
	var contentPtr *string
	// OpenAI spec: `content` is nullable. It should be null if `tool_calls` is present and there's no text content.
	if contentStr != "" || (len(openAIToolCalls) == 0 && contentStr == "") {
		// Set content if it's non-empty, OR if there are no tool calls (even if content is empty, role needs to be there)
		contentPtr = &contentStr
	}

	// If there were tool calls, content might be null.
	// If there was an error from Poe, we've appended it to contentStr.

	openAIResponse := &types.OpenAIChatCompletionResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: createdTimestamp,
		Model:   requestModelID,
		Choices: []types.OpenAIChoice{
			{
				Index: 0,
				Message: types.OpenAIResponseMessage{
					Role:      finalRole, // Should always be "assistant"
					Content:   contentPtr,
					ToolCalls: openAIToolCalls,
				},
				FinishReason: finalFinishReason,
			},
		},
		Usage: &types.OpenAIUsage{ // Poe does not provide token usage.
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}
	slog.Debug(
		"Successfully aggregated Poe events to OpenAI non-streaming response",
		"response_id",
		openAIResponse.ID,
	)
	return openAIResponse, nil
}

// GenerateID creates a unique identifier string with a given prefix.
// It combines the prefix, current nanosecond timestamp, and a short pseudo-random string.
// Note: pseudoRandString is not cryptographically secure.
func GenerateID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), pseudoRandString(8))
}

// pseudoRandString generates a pseudo-random string of length n.
// This is not cryptographically secure and is used for generating simple unique-enough IDs.
func pseudoRandString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	// Seed with time for some variability, though still not truly random.
	// A proper random number generator (e.g., crypto/rand) should be used for security-sensitive IDs.
	// For this application's internal IDs, this basic approach might suffice.
	// Consider replacing with UUID if stronger uniqueness is required.
	source := pseudoRandSource{val: time.Now().UnixNano()}
	for i := range b {
		b[i] = letters[source.Int63()%int64(len(letters))]
	}
	return string(b)
}

// pseudoRandSource is a simple pseudo-random source for generating IDs.
// This is to avoid Go's global math/rand source which can have contention
// and to make it clear this is not for cryptographic purposes.
type pseudoRandSource struct {
	val int64
}

func (s *pseudoRandSource) Int63() int64 {
	s.val = s.val*48271 + time.Now().UnixNano() // Basic LCG-like step, mixing with time
	if s.val < 0 {
		s.val = -s.val
	}
	return s.val
}
