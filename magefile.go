//go:build mage

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/magefile/mage/sh"
)

// Lint delegates linting to the magefile in the tools directory.
func Lint() error {
	log.Println("Delegating lint to tools...")
	return sh.RunV("mage", "-d", "./tools", "lint")
}

// Serve delegates serving to the magefile in the tools directory.
// dirpath is the path to the application directory to be served (e.g., apps/client).
func Serve(dirpath string) error {
	log.Printf("Delegating serve for %s to tools...\n", dirpath)
	return sh.RunV("mage", "-d", "./tools", "serve", dirpath)
}

// Install syncs Go workspace and then delegates installation to the tools magefile.
func Install() error {
	log.Println("Syncing Go workspace...")
	if err := sh.RunV("go", "work", "sync"); err != nil {
		return fmt.Errorf("go work sync failed: %w", err)
	}
	log.Println("Delegating install to tools...")
	return sh.RunV("mage", "-d", "./tools", "install")
}

// Build creates the 'dist' directory and then delegates building the specified module
// to the Build command in the tools directory's magefile.
// moduleMainGoPath is the path to the module's main.go file (e.g., "apps/poepenai/main.go").
func Build(moduleMainGoPath string) error {
	log.Println("Ensuring 'dist' directory exists in project root...")
	if err := os.MkdirAll("dist", os.ModePerm); err != nil {
		return fmt.Errorf("failed to create dist directory from root magefile: %w", err)
	}

	log.Printf("Delegating build for %s to tools...\n", moduleMainGoPath)
	return sh.RunV("mage", "-d", "./tools", "Build", moduleMainGoPath)
}
