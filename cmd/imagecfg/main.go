// imagecfg.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/osbuild/blueprint/pkg/blueprint"
	"github.com/spf13/cobra"
)

const defaultBlueprintPath = "/usr/lib/bootc-image-builder/config.toml"

// --- Blueprint Parsing Helper ---
func parseBlueprint(path string) (*blueprint.Blueprint, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening blueprint file %s: %w", path, err)
	}
	defer file.Close()

	var bp blueprint.Blueprint
	decoder := toml.NewDecoder(file)
	meta, err := decoder.Decode(&bp)
	if err != nil {
		return nil, fmt.Errorf("error parsing blueprint TOML from %s: %w", path, err)
	}

	// Check for undecoded keys
	if len(meta.Undecoded()) > 0 {
		var unknownKeys []string
		for _, key := range meta.Undecoded() {
			unknownKeys = append(unknownKeys, key.String())
		}
		return nil, fmt.Errorf("unknown configuration keys in %s: %s", path, strings.Join(unknownKeys, ", "))
	}

	return &bp, nil
}

// Helper function to load blueprint
func loadBlueprint(args []string) (*blueprint.Blueprint, error) {
	blueprintPath := defaultBlueprintPath
	if len(args) > 0 {
		blueprintPath = args[0]
	}
	bp, err := parseBlueprint(blueprintPath)
	if err != nil {
		return nil, err // Already includes path info
	}
	return bp, nil
}

// --- Cobra Setup ---
var rootCmd = &cobra.Command{
	Use:   "imagecfg",
	Short: "imagecfg is a tool for working with OSBuild blueprints",
	Long:  `A command-line utility to process OSBuild blueprints, for example, to translate them into other formats like bash scripts.`,
}

var bashCmd = &cobra.Command{
	Use:   "bash [blueprint.toml]",
	Short: "Translate an OSBuild blueprint to a bash script",
	Long: `Translates an OSBuild blueprint (TOML format) into a bash script
that attempts to apply the configurations.

If no blueprint path is provided, the default path (/usr/lib/bootc-image-builder/config.toml) will be used.

Supported configurations:
- packages
- user
- group
- hostname
- timezone
- firewall (ports, enabled services)
- locale
- services (enabled/disabled)

The generated script should be reviewed carefully before execution.
Each customization type is translated into a block of bash commands.
If multiple commands are needed for a single logical step, they are chained with '&&'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		bp, err := loadBlueprint(args)
		if err != nil {
			return err // Cobra will print this and exit
		}

		header, namedBlocks, err := generateBashScript(bp)
		if err != nil {
			return fmt.Errorf("error generating bash script: %w", err)
		}

		var fullScript strings.Builder
		fullScript.WriteString(header)
		if len(namedBlocks) > 0 {
			fullScript.WriteString("\n") // Add a newline before the first command block
			var commandStrings []string
			for _, nb := range namedBlocks {
				if nb.Commands != "" {
					commandStrings = append(commandStrings, nb.Commands)
				}
			}
			fullScript.WriteString(strings.Join(commandStrings, "\n\n"))
			fullScript.WriteString("\n") // Add a newline after the last command block
		}
		fmt.Println(fullScript.String())
		return nil
	},
}

var applyCmd = &cobra.Command{
	Use:   "apply [blueprint.toml]",
	Short: "Apply an OSBuild blueprint directly",
	Long: `Applies an OSBuild blueprint (TOML format) by generating and executing
a bash script that implements the configurations.

If no blueprint path is provided, the default path (/usr/lib/bootc-image-builder/config.toml) will be used.

This command requires root privileges as it modifies system configuration.
The same configurations are supported as in the 'bash' command.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		bp, err := loadBlueprint(args)
		if err != nil {
			return err // Cobra will print this and exit
		}

		header, namedBlocks, err := generateBashScript(bp)
		if err != nil {
			return fmt.Errorf("error generating command blocks: %w", err)
		}

		if len(namedBlocks) == 0 {
			fmt.Println("No configurations to apply.")
			return nil
		}

		for _, block := range namedBlocks {
			if strings.TrimSpace(block.Commands) == "" {
				continue // Skip empty command blocks
			}

			fmt.Printf("Applying: %s...\n", block.Name)

			// Create a temporary script file for this block
			tmpfile, err := os.CreateTemp("", "imagecfg-block-*.sh")
			if err != nil {
				return fmt.Errorf("error creating temporary script for '%s': %w", block.Name, err)
			}
			// Defer removal of the temp file. This runs when the RunE function returns.
			// Using a func literal to capture the current tmpfile.Name().
			defer func(name string) {
				if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
					// Log error during deferred removal, but don't override original error
					fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary script %s during deferred cleanup: %v\n", name, err)
				}
			}(tmpfile.Name())

			// Write the header and current command block to the temporary file
			blockScript := header + "\n" + block.Commands
			if _, err := tmpfile.WriteString(blockScript); err != nil {
				_ = tmpfile.Close() // Attempt to close, ignore error as we are in an error path.
				return fmt.Errorf("error writing script for '%s' to %s: %w", block.Name, tmpfile.Name(), err)
			}
			if err := tmpfile.Close(); err != nil {
				return fmt.Errorf("error closing temporary file for '%s' (%s): %w", block.Name, tmpfile.Name(), err)
			}

			// Make the script executable
			if err := os.Chmod(tmpfile.Name(), 0755); err != nil {
				return fmt.Errorf("error making script for '%s' (%s) executable: %w", block.Name, tmpfile.Name(), err)
			}

			// Execute the script
			execCmd := exec.Command(tmpfile.Name())
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr // Capture stderr for error reporting
			if err := execCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "\n--- ERROR: Failed to apply '%s' ---\n", block.Name)
				fmt.Fprintf(os.Stderr, "Error details: %v\n", err)
				fmt.Fprintf(os.Stderr, "Attempted commands for '%s':\n%s\n", block.Name, block.Commands)
				fmt.Fprintf(os.Stderr, "--- END ERROR ---\n")
				return fmt.Errorf("execution failed for block '%s'", block.Name) // Error returned, defer will clean up tmpfile
			}
			fmt.Printf("Successfully applied: %s\n", block.Name)
			// Temp file for this successful block will be cleaned up by the deferred call when RunE exits.
		}
		fmt.Println("\nAll configurations applied successfully.")
		return nil
	},
}

// Execute executes the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Cobra automatically prints the error to os.Stderr if RunE returns an error.
		// We just need to ensure the process exits with an error code.
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(bashCmd)
	rootCmd.AddCommand(applyCmd)
}

// --- Main Application Logic ---
func main() {
	Execute()
}

// NamedCommandBlock holds a named block of commands
type NamedCommandBlock struct {
	Name     string
	Commands string
}

// --- Bash Script Generation Orchestrator ---
func generateBashScript(bp *blueprint.Blueprint) (string, []NamedCommandBlock, error) {
	var scriptHeader strings.Builder
	var namedCommandBlocks []NamedCommandBlock

	// --- Script Header ---
	scriptHeader.WriteString("#!/bin/bash\n")
	scriptHeader.WriteString("set -euf -o pipefail\n\n") // Exit on error, unset var, fail on pipe error, no glob

	// --- vibe-coding: Bash script generation so chill, even your TOML wants to dance.
	// --- Higher-order function inside a function, passing functions to functions, all to generate bash from TOML.
	type blockGen struct {
		name      string
		generator func(*blueprint.Blueprint) (string, error)
	}

	blockGenerators := []blockGen{
		{"Packages", generatePackagesCmd},
		{"Hostname", generateHostnameCmd},
		{"Timezone", generateTimezoneCmd},
		{"Locale", generateLocaleCmd},
		{"Groups", generateGroupsBlockCmd},
		{"Users", generateUsersBlockCmd},
		{"Firewall", generateFirewallCmd},
		{"Services", generateServicesCmd},
	}

	for _, blk := range blockGenerators {
		cmdStr, err := blk.generator(bp)
		if err != nil {
			return "", nil, fmt.Errorf("could not generate commands for %s: %w", blk.name, err)
		}
		if cmdStr != "" {
			namedCommandBlocks = append(namedCommandBlocks, NamedCommandBlock{Name: blk.name, Commands: cmdStr})
		}
	}

	// Script generation no longer assembles the final script here.
	// It returns the header and the blocks separately.
	return scriptHeader.String(), namedCommandBlocks, nil
}
