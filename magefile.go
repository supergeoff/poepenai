//go:build mage

package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// run executes the given command with arguments in the specified workDir.
// If workDir is empty, it runs in the current directory.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}

// Lint delegates linting to the magefile in the tools directory.
func Lint() error {
	args := []string{
		"run",
		"--build-tags=mage", // Include files with 'mage' build tag if any.
		"./...",             // Lint all packages within the target directory.
	}

	slog.Info("Running golangci-lint", "arguments", args)
	err := run("golangci-lint", args...)
	if err != nil {
		slog.Error(
			"golangci-lint failed",
			"error", err,
		)
		// Return a new error that includes the module path for clarity to the caller.
		return fmt.Errorf("golangci-lint failed: %w", err)
	}
	slog.Info("Finished linting module")
	return nil
}

// Serve delegates serving to the magefile in the tools directory.
// dirpath is the path to the application directory to be served (e.g., apps/client).
func Serve() error {
	slog.Info("Serving application")
	return run("go", "tool", "air", "-c", ".air.toml")
}

// Build creates the 'dist' directory and then delegates building the specified module
// to the Build command in the tools directory's magefile.
// moduleMainGoPath is the path to the module's main.go file (e.g., "apps/poepenai/main.go").
func Build() error {
	log.Println("Ensuring 'dist' directory exists in project root...")
	if err := os.MkdirAll("dist", os.ModePerm); err != nil {
		return fmt.Errorf("failed to create dist directory from root magefile: %w", err)
	}

	outputForGoBuildOpt := filepath.Join(
		"dist",
		"poepenai",
	)
	outputForGoBuildOpt = filepath.Clean(
		outputForGoBuildOpt,
	)

	err := run("go", "build", "-o", outputForGoBuildOpt, ".")
	if err != nil {
		slog.Error("Failed to build application",
			"error", err,
		)
		return fmt.Errorf("failed to build module: %w", err)
	}

	slog.Info(
		"Successfully built application",
		"output_location", outputForGoBuildOpt, // More user-friendly path for log.
	)
	return nil
}
