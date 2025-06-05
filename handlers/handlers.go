package handlers

import (
	"html/template"
	"log/slog"

	"github.com/supergeoff/poepenai/client"
	"github.com/supergeoff/poepenai/service"
)

// AppHandlers holds dependencies for HTTP handlers.
type AppHandlers struct {
	Logger           *slog.Logger
	PoeClient        *client.PoeClient
	RingBufferLogger *service.RingBufferLogWriter
	LogsTemplate     *template.Template
}

// NewAppHandlers creates a new AppHandlers struct with its dependencies.
func NewAppHandlers(
	logger *slog.Logger,
	poeClient *client.PoeClient,
	ringBufferLogger *service.RingBufferLogWriter,
	logsTemplate *template.Template,
) *AppHandlers {
	return &AppHandlers{
		Logger:           logger,
		PoeClient:        poeClient,
		RingBufferLogger: ringBufferLogger,
		LogsTemplate:     logsTemplate,
	}
}
