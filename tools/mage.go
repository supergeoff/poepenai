//go:build mage

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// run executes the given command with arguments in the specified workDir.
// If workDir is empty, it runs in the current directory.
func run(workDir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if workDir != "" { // Set working directory if provided
		cmd.Dir = workDir
	}

	return cmd.Run()
}

// getWorkspaceModules reads the go.work file (expected at ../go.work relative to tools/)
// and returns a list of module paths defined in it.
func getWorkspaceModules() ([]string, error) {
	workFilePath := filepath.Join(
		"..",
		"go.work",
	) // Assumes tools/ is one level down from project root.
	data, err := os.ReadFile(workFilePath)
	if err != nil {
		wd, _ := os.Getwd() // For richer error logging.
		slog.Error(
			"Failed to read go.work file",
			"path", workFilePath,
			"cwd", wd,
			"error", err, // Changed "original_error" to "error" for consistency with slog.
		)
		return nil, fmt.Errorf("failed to read go.work file %s: %w", workFilePath, err)
	}

	wf, err := modfile.ParseWork(filepath.Base(workFilePath), data, nil)
	if err != nil {
		slog.Error(
			"Failed to parse go.work file",
			"path", workFilePath,
			"error", err,
		)
		return nil, fmt.Errorf("failed to parse go.work file %s: %w", workFilePath, err)
	}

	var modules []string
	for _, use := range wf.Use {
		modules = append(modules, use.Path)
	}
	return modules, nil
}

func Lint() error {
	slog.Info("Reading modules from go.work...")
	modules, err := getWorkspaceModules()
	if err != nil {
		// getWorkspaceModules already logs details and returns a wrapped error.
		return errors.New("could not get workspace modules") // This is fine as a simple message.
	}

	if len(modules) == 0 {
		slog.Info("No modules found in go.work. Nothing to lint.")
		return nil
	}

	slog.Info("Found modules to lint", "modules", modules)
	slog.Info("Linting Go modules in workspace...")

	for _, modulePath := range modules {
		slog.Info("Linting module", "module", modulePath)
		// modulePath from go.work is relative to project root (e.g., "apps/client").
		// We need to run golangci-lint in that directory.
		// relModuleDir is the path to the module directory relative to tools/ (e.g., "../apps/client").
		relModuleDir := filepath.Join("..", filepath.Clean(modulePath))

		args := []string{
			"run",
			"--build-tags=mage", // Include files with 'mage' build tag if any.
			"./...",             // Lint all packages within the target directory.
		}

		slog.Info("Running golangci-lint", "in_directory", relModuleDir, "arguments", args)
		err := run(relModuleDir, "golangci-lint", args...)
		if err != nil {
			slog.Error(
				"golangci-lint failed for module",
				"module", modulePath,
				"directory", relModuleDir,
				"error", err,
			)
			// Return a new error that includes the module path for clarity to the caller.
			return fmt.Errorf("golangci-lint failed for module %s: %w", modulePath, err)
		}
		slog.Info("Finished linting module", "module", modulePath)
	}

	slog.Info("All modules linted successfully.")
	return nil
}

// Build compiles the Go application specified by moduleMainGoPath (path to its main.go from project root)
// and places the binary in the PROJECT_ROOT/dist/ directory.
// The binary name is derived from the first directory under "apps/".
// Example: mage Build apps/poepenai/main.go -> builds PROJECT_ROOT/dist/poepenai
// Example: mage Build apps/server/cmd/api/main.go -> builds PROJECT_ROOT/dist/server
func Build(moduleMainGoPath string) error {
	slog.Info("Building application", "main_go_path", moduleMainGoPath)

	cleanPath := filepath.Clean(moduleMainGoPath) // e.g., "apps/poepenai/main.go"
	parts := strings.Split(cleanPath, string(filepath.Separator))

	if len(parts) < 2 || parts[0] != "apps" {
		// Error creation using fmt.Errorf is appropriate here as it includes dynamic value.
		err := fmt.Errorf(
			"moduleMainGoPath must be under 'apps/' directory (e.g., 'apps/myapp/main.go'), got: %s",
			moduleMainGoPath,
		)
		slog.Error("Invalid module path for build", "error", err.Error()) // Log the error string.
		return err
	}
	// moduleName is the first directory under "apps", e.g., "poepenai" or "server".
	moduleName := parts[1]

	// buildDirRelToRoot is the directory containing main.go, relative to project root.
	// e.g., "apps/poepenai" or "apps/server/cmd/api".
	buildDirRelToRoot := filepath.Dir(cleanPath)

	// Note: The 'dist' directory itself is created by the root magefile.
	// distDirRelToTools was previously declared here but is no longer used.

	// workDirForGoBuild is where 'go build' will be executed, relative to tools/.
	// e.g., "../apps/poepenai" or "../apps/server/cmd/api".
	workDirForGoBuild := filepath.Join("..", buildDirRelToRoot)

	// outputBinaryName is the simple name of the binary file, e.g., "poepenai".
	outputBinaryName := moduleName

	// outputForGoBuildOpt is the -o argument for 'go build', relative to workDirForGoBuild.
	// It needs to point to PROJECT_ROOT/dist/outputBinaryName.
	// Calculate depth of buildDirRelToRoot from project_root.
	buildDirDepth := len(strings.Split(buildDirRelToRoot, string(filepath.Separator)))
	// Construct path from workDirForGoBuild up to project_root, then down to dist/outputBinaryName.
	pathToRootFromWorkDir := strings.Repeat(".."+string(filepath.Separator), buildDirDepth)
	outputForGoBuildOpt := filepath.Join(
		pathToRootFromWorkDir,
		"dist",
		outputBinaryName,
	) // Corrected: added :=
	outputForGoBuildOpt = filepath.Clean(
		outputForGoBuildOpt,
	) // Resolve ".."

	slog.Info("Build parameters calculated",
		"module_name", moduleName,
		"build_dir_from_root", buildDirRelToRoot,
		"go_build_exec_dir_from_tools", workDirForGoBuild,
		"go_build_output_path_from_exec_dir", outputForGoBuildOpt,
	)

	// Execute 'go build'. The '.' means build the package in the current working directory (workDirForGoBuild).
	err := run(workDirForGoBuild, "go", "build", "-o", outputForGoBuildOpt, ".")
	if err != nil {
		slog.Error("Failed to build application",
			"module", moduleName,
			"path_to_main", moduleMainGoPath,
			"error", err,
		)
		return fmt.Errorf("failed to build module %s: %w", moduleName, err)
	}

	// For logging the final absolute-like path from project root.
	finalBinaryUserPath := filepath.Join("dist", outputBinaryName)
	slog.Info(
		"Successfully built application",
		"module", moduleName,
		"output_location", finalBinaryUserPath, // More user-friendly path for log.
	)
	return nil
}

// Serve delegates serving the application specified by dirpath (e.g., "apps/client")
// to the 'go tool air -c .air.toml' command run within that directory.
func Serve(dirpath string) error {
	slog.Info("Serving application", "directory", dirpath)
	// dirpath is relative to project root.
	// relModuleDir is path to module dir relative to tools/.
	relModuleDir := filepath.Join("..", filepath.Clean(dirpath))
	return run(relModuleDir, "go", "tool", "air", "-c", ".air.toml")
}

// Install downloads and installs the Tailwind CSS CLI tool into the tools/ directory.
// It checks if the tool is already present and executable.
func Install() error {
	// finalPath is relative to the tools/ directory (where this magefile runs).
	finalPath := "tailwindcss"

	// Check if the tool already exists and is executable.
	if info, err := os.Stat(finalPath); err == nil {
		if !info.IsDir() && (info.Mode().Perm()&0o111 != 0) { // Is a file and executable.
			slog.Info(
				"Tailwind CSS already installed and executable. Skipping installation.",
				"path",
				finalPath,
			)
			return nil
		}
		slog.Warn(
			"Found Tailwind CSS in tools, but it's not a valid executable or has wrong permissions. Proceeding with re-installation.",
			"path",
			finalPath,
		)
		if err := os.RemoveAll(finalPath); err != nil {
			slog.Error(
				"Failed to remove existing non-executable Tailwind CSS",
				"path",
				finalPath,
				"error",
				err,
			)
			return fmt.Errorf("failed to remove existing non-executable %s: %w", finalPath, err)
		}
	} else if !os.IsNotExist(err) {
		slog.Error("Failed to check status of Tailwind CSS executable", "path", finalPath, "error", err)
		return fmt.Errorf("failed to check status of %s: %w", finalPath, err)
	}
	// If os.IsNotExist(err) is true, or an invalid existing file was removed, proceed.

	slog.Info("Installing Tailwind CSS to tools/ directory...")

	tailwindURL := "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.1.7/tailwindcss-linux-x64"
	downloadPath := "tailwindcss-linux-x64" // Temporary download name in tools/

	_ = os.Remove(downloadPath) // Clean up potentially existing temporary download file.

	slog.Info("Downloading Tailwind CSS...", "url", tailwindURL, "destination", downloadPath)
	// 'run' with empty workDir executes in current directory (tools/).
	err := run("", "curl", "-sLO", tailwindURL)
	if err != nil {
		slog.Error("Failed to download Tailwind CSS", "url", tailwindURL, "error", err)
		return errors.New("failed to download Tailwind CSS") // Simple error message.
	}

	slog.Info("Renaming downloaded file...", "from", downloadPath, "to", finalPath)
	if err := os.Rename(downloadPath, finalPath); err != nil {
		_ = os.Remove(downloadPath) // Clean up.
		slog.Error(
			"Failed to rename downloaded Tailwind CSS file",
			"from",
			downloadPath,
			"to",
			finalPath,
			"error",
			err,
		)
		return fmt.Errorf("failed to rename %s to %s: %w", downloadPath, finalPath, err)
	}

	slog.Info("Making Tailwind CSS executable...", "path", finalPath)
	if err := os.Chmod(finalPath, 0o755); err != nil {
		_ = os.Remove(finalPath) // Clean up.
		slog.Error("Failed to make Tailwind CSS executable", "path", finalPath, "error", err)
		return fmt.Errorf("failed to make %s executable: %w", finalPath, err)
	}

	slog.Info("Tailwind CSS installed successfully.", "path", finalPath)
	return nil
}
