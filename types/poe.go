package types

// PoeFeedbackType represents the type of feedback for a message in the Poe protocol.
// Possible values are "like" or "dislike".
type PoeFeedbackType string

// PoeContentType represents the content type of a message in the Poe protocol.
// Possible values are "text/markdown" or "text/plain".
type PoeContentType string

// PoeProtocolMessage represents a single message in a conversation according to the Poe protocol.
// It is used in the Query field of PoeQueryRequest.
type PoeProtocolMessage struct {
	// Role of the message author. Must be one of "system", "user", or "bot".
	Role string `json:"role"`
	// Content of the message.
	Content string `json:"content"`
	// ContentType of the message, defaults to "text/markdown".
	ContentType string `json:"content_type,omitempty"`
	// Timestamp of the message, typically Unix epoch time in milliseconds or seconds.
	Timestamp int64 `json:"timestamp,omitempty"`
	// MessageID is a unique identifier for the message.
	MessageID string `json:"message_id,omitempty"`
	// TODO: Attachments []PoeAttachment `json:"attachments,omitempty"` // For future use if needed
}

// PoeToolFunctionParamsDefinition defines the JSON schema for parameters of a Poe tool function.
// This mirrors OpenAI's function parameter definition.
type PoeToolFunctionParamsDefinition struct {
	// Type of the parameters object, typically "object".
	Type string `json:"type"`
	// Properties of the parameters object, described as a JSON schema.
	Properties map[string]interface{} `json:"properties"`
	// Required parameters.
	Required []string `json:"required,omitempty"`
}

// PoeToolFunctionDefinition defines a Poe tool of type "function".
// This mirrors OpenAI's function definition.
type PoeToolFunctionDefinition struct {
	// Name of the function to be called.
	Name string `json:"name"`
	// Description of what the function does.
	Description string `json:"description"`
	// Parameters the function accepts, described as a JSON Schema object.
	Parameters PoeToolFunctionParamsDefinition `json:"parameters"`
}

// PoeToolDefinition defines a tool that a Poe bot can be instructed to use.
// This mirrors OpenAI's tool definition.
type PoeToolDefinition struct {
	// Type of the tool. Currently, only "function" is supported.
	Type string `json:"type"`
	// Function definition.
	Function PoeToolFunctionDefinition `json:"function"`
}

// PoeQueryRequest is the structure sent to a Poe bot to request a chat completion.
type PoeQueryRequest struct {
	// Version of the Poe protocol, e.g., "1.1".
	Version string `json:"version"`
	// Type of the request, always "query" for chat completions.
	Type string `json:"type"`
	// Query is a list of messages representing the current conversation history.
	Query []PoeProtocolMessage `json:"query"`
	// UserID is an anonymized identifier for the user.
	UserID string `json:"user_id"`
	// ConversationID is an identifier for the current chat session.
	ConversationID string `json:"conversation_id"`
	// MessageID is a unique identifier for this specific query request.
	MessageID string `json:"message_id"`
	// APIKey is the Poe Platform API Key used by our adapter to authenticate with the Poe API gateway.
	// This key is part of the JSON payload sent to the Poe bot.
	APIKey string `json:"api_key"`
	// Temperature for sampling, influences randomness.
	Temperature *float64 `json:"temperature,omitempty"`
	// SkipSystemPrompt indicates whether the bot should ignore its predefined system prompt.
	SkipSystemPrompt bool `json:"skip_system_prompt,omitempty"`
	// LogitBias modifies the likelihood of specified tokens appearing.
	LogitBias map[string]float64 `json:"logit_bias,omitempty"`
	// StopSequences are sequences where the bot should stop generating tokens.
	StopSequences []string `json:"stop_sequences,omitempty"`
	// Tools is a list of tools the bot may be instructed to use.
	Tools []PoeToolDefinition `json:"tools,omitempty"`
	// Note: The Poe server bot (built with fastapi_poe) expects an 'access_key' in the
	// QueryRequest for its own validation if it's configured with one.
	// Our adapter, when acting as a client to the Poe Platform API (api.poe.com/bot/),
	// uses the APIKey field in the payload and also as a Bearer token in the HTTP Authorization header.
}

// PoePartialResponseData is the structure of the JSON data field for Poe SSE events
// like "text", "replace_response", "suggested_reply", and "json".
type PoePartialResponseData struct {
	// Text content of the partial response or suggested reply.
	Text string `json:"text"`
	// Data field, typically used by "json" events to send structured data,
	// such as tool call information (often mimicking OpenAI's chunk structure).
	Data map[string]interface{} `json:"data,omitempty"`
	// IsSuggestedReply indicates if this partial response is a suggested reply.
	IsSuggestedReply bool `json:"is_suggested_reply,omitempty"`
	// IsReplaceResponse indicates if this response should replace the previous bot message.
	IsReplaceResponse bool `json:"is_replace_response,omitempty"`
	// TODO: Attachment *PoeAttachmentData `json:"attachment,omitempty"` // If bot sends an attachment directly
}

// PoeMetaEventData is the structure of the JSON data field for a "meta" SSE event from a Poe bot.
// It provides metadata about the response stream.
type PoeMetaEventData struct {
	// ContentType of the bot's response, e.g., "text/markdown".
	ContentType PoeContentType `json:"content_type,omitempty"`
	// RefetchSettings indicates if the client should refetch bot settings.
	RefetchSettings bool `json:"refetch_settings,omitempty"`
	// Linkify indicates if plaintext URLs in the response should be linkified (deprecated by Poe).
	Linkify bool `json:"linkify,omitempty"`
	// SuggestedReplies indicates if the bot may send suggested replies.
	SuggestedReplies bool `json:"suggested_replies,omitempty"`
}

// PoeErrorEventData is the structure of the JSON data field for an "error" SSE event from a Poe bot.
type PoeErrorEventData struct {
	// AllowRetry indicates if the client is allowed to retry the request.
	AllowRetry bool `json:"allow_retry,omitempty"`
	// Text is a human-readable error message.
	Text *string `json:"text,omitempty"`
	// ErrorType is a machine-readable error type, e.g., "user_message_too_long".
	ErrorType *string `json:"error_type,omitempty"`
}

// PoeFileEventData is the structure of the JSON data field for a "file" SSE event from a Poe bot.
// This is used if the bot sends a file as part of its response.
type PoeFileEventData struct {
	// URL of the uploaded file.
	URL string `json:"url"`
	// ContentType (MIME type) of the file.
	ContentType string `json:"content_type"`
	// Name of the file.
	Name string `json:"name"`
	// InlineRef is a reference for inline attachments.
	InlineRef *string `json:"inline_ref,omitempty"`
}

// PoeSSEEvent is a generic wrapper for a Server-Sent Event received from a Poe bot.
// The Data field contains a JSON string whose structure depends on the Event type.
type PoeSSEEvent struct {
	// Event type, e.g., "text", "json", "meta", "error", "done".
	Event string
	// Data is the raw JSON string payload of the event.
	Data string
}
