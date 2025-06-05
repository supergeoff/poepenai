package cmd

import (
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/supergeoff/poepenai/client"
	"github.com/supergeoff/poepenai/handlers"
	"github.com/supergeoff/poepenai/service"
)

const (
	templatesDir = "templates" // Relative path for templates
	AppVersion   = "0.1.0"     // Application version
)

// Flags
var logLevelFlag string

// Global instances for dependencies
var (
	logger           *slog.Logger
	poeClient        *client.PoeClient
	ringBufferLogger *service.RingBufferLogWriter
	logsTemplate     *template.Template
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "poe_adapter",
	Short: "An OpenAI-compatible API adapter for Poe.",
	Long:  `Proxies OpenAI API requests to Poe bots, providing an OpenAI-compatible interface.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize ringBufferLogger first as it's needed by multiWriter
		ringBufferLogger = service.NewRingBufferLogWriter() // Default size
		multiWriter := io.MultiWriter(os.Stdout, ringBufferLogger)

		programLogLevel := new(slog.LevelVar) // Default is Info
		switch strings.ToLower(logLevelFlag) {
		case "debug":
			programLogLevel.Set(slog.LevelDebug)
		case "info":
			programLogLevel.Set(slog.LevelInfo)
		case "warn":
			programLogLevel.Set(slog.LevelWarn)
		case "error":
			programLogLevel.Set(slog.LevelError)
		default:
			// Use a temporary basic logger for this initial error message
			// This will print to Stderr if the main logger isn't set up yet.
			slog.Error(
				"Invalid log level specified. Defaulting to INFO.",
				"specified_level",
				logLevelFlag,
			)
			programLogLevel.Set(slog.LevelInfo)
		}

		logger = slog.New(
			slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{Level: programLogLevel}),
		)
		slog.SetDefault(logger) // Make it the default for any package-level slog calls

		// Initialize other dependencies
		poeClient = client.NewPoeClient()

		// Load HTML templates
		var err error
		templatePathsToTry := []string{
			filepath.Join(
				"..",
				templatesDir,
				"logger.html",
			), // From cmd/ up to poepenai/templates/
			filepath.Join("apps", "poepenai", templatesDir, "logger.html"), // From project root
			filepath.Join(
				templatesDir,
				"logger.html",
			), // Relative to CWD (e.g. if CWD is cmd/)
			"../templates/logger.html", // Simpler relative for common case
			"templates/logger.html",    // If templates is in CWD
		}

		var templateLoaded bool
		for _, p := range templatePathsToTry {
			logsTemplate, err = template.ParseFiles(p)
			if err == nil {
				logger.Info("Successfully parsed logs template", "path", p)
				templateLoaded = true
				break
			}
			logger.Debug("Failed to parse logs template at path", "error", err, "path_tried", p)
		}
		if !templateLoaded {
			logger.Warn(
				"All attempts to parse logs template failed. Logs page will not render correctly.",
			)
			// logsTemplate will remain nil, handlers should check for this.
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// If no subcommand is given, print help for the root command.
		if err := cmd.Help(); err != nil {
			slog.Error("Failed to display help", "error", err)
		}
	},
}

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the Poe OpenAI Adapter server",
	Long:  `Initializes and starts the HTTP server that listens for OpenAI API requests.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// logger, poeClient, ringBufferLogger, logsTemplate are initialized by rootCmd.PersistentPreRunE.
		if logger == nil {
			return fmt.Errorf(
				"logger not initialized, PersistentPreRunE might have failed or was skipped",
			)
		}
		if poeClient == nil {
			return fmt.Errorf("poe client not initialized")
		}
		// ringBufferLogger and logsTemplate can be nil if their setup failed, handlers should manage this.

		appHandlers := handlers.NewAppHandlers(logger, poeClient, ringBufferLogger, logsTemplate)

		r := chi.NewRouter()
		r.Use(middleware.RequestID)
		r.Use(middleware.RealIP)
		// Using slog for request logging via a custom middleware would be ideal.
		// For now, Chi's logger will provide some basic request logging.
		r.Use(middleware.Logger)
		r.Use(middleware.Recoverer)
		r.Use(middleware.Timeout(60 * time.Second))

		r.Post("/v1/chat/completions", appHandlers.HandleChatCompletions)
		r.Get("/logs", appHandlers.HandleLogsPage)

		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}

		logger.Info("Starting Poe OpenAI Adapter server", "port", port, "log_level", logLevelFlag)
		if err := http.ListenAndServe(":"+port, r); err != nil {
			logger.Error("Failed to start server", "error", err)
			return fmt.Errorf("failed to start server: %w", err)
		}
		return nil
	},
}

func init() {
	// Persistent flags are available to the command and all its children
	rootCmd.PersistentFlags().
		StringVar(&logLevelFlag, "loglevel", "info", "Log level (debug, info, warn, error)")

	// Set the version for the --version flag
	rootCmd.Version = AppVersion

	// Add subcommands
	rootCmd.AddCommand(startCmd)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Cobra's default behavior is to print the error to os.Stderr.
		// If rootCmd.SilenceErrors is true, we might need to print it here.
		// For now, rely on Cobra's default.
		// Ensure a non-zero exit code on error.
		os.Exit(1)
	}
}
