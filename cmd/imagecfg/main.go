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

// --- Cobra Setup ---
var rootCmd = &cobra.Command{
	Use:   "imagecfg",
	Short: "imagecfg is a tool for working with OSBuild blueprints",
	Long:  `A command-line utility to process OSBuild blueprints, for example, to translate them into other formats like bash scripts.`,
}

// Common function to handle blueprint loading and script generation
func handleBlueprintAndScript(args []string) (string, error) {
	blueprintPath := defaultBlueprintPath
	if len(args) > 0 {
		blueprintPath = args[0]
	}

	bp, err := parseBlueprint(blueprintPath)
	if err != nil {
		return "", err
	}

	script, err := generateBashScript(bp)
	if err != nil {
		return "", fmt.Errorf("error generating bash script: %w", err)
	}

	return script, nil
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
	Run: func(cmd *cobra.Command, args []string) {
		script, err := handleBlueprintAndScript(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(script)
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
	Run: func(cmd *cobra.Command, args []string) {
		script, err := handleBlueprintAndScript(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

		// Create a temporary script file
		tmpfile, err := os.CreateTemp("", "imagecfg-*.sh")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating temporary script: %v\n", err)
			os.Exit(1)
		}
		defer os.Remove(tmpfile.Name())

		// Write the script to the temporary file
		if _, err := tmpfile.WriteString(script); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing script: %v\n", err)
			os.Exit(1)
		}
		if err := tmpfile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing temporary file: %v\n", err)
			os.Exit(1)
		}

		// Make the script executable
		if err := os.Chmod(tmpfile.Name(), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error making script executable: %v\n", err)
			os.Exit(1)
		}

		// Execute the script
		execCmd := exec.Command(tmpfile.Name())
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing script: %v\n", err)
			os.Exit(1)
		}
	},
}

// Execute executes the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
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

// --- Bash Script Generation Orchestrator ---
func generateBashScript(bp *blueprint.Blueprint) (string, error) {
	var scriptHeader strings.Builder
	var commandBlocks []string // Each item will be a block of commands for a major customization type

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
		{"Hostname", generateHostnameCmd},
		{"Timezone", generateTimezoneCmd},
		{"Locale", generateLocaleCmd},
		{"Groups", generateGroupsBlockCmd},
		{"Users", generateUsersBlockCmd},
		{"Firewall", generateFirewallCmd},
		{"Services", generateServicesCmd},
		{"Packages", generatePackagesCmd},
	}

	for _, blk := range blockGenerators {
		cmdStr, err := blk.generator(bp)
		if err != nil {
			return "", fmt.Errorf("could not generate commands for %s: %w", blk.name, err)
		}
		if cmdStr != "" {
			commandBlocks = append(commandBlocks, cmdStr)
		}
	}

	// --- Final Script Assembly ---
	var finalScript strings.Builder
	finalScript.WriteString(scriptHeader.String())
	if len(commandBlocks) > 0 {
		finalScript.WriteString("\n") // Add a newline before the first command block
		// Join blocks, each separated by two newlines for readability
		finalScript.WriteString(strings.Join(commandBlocks, "\n\n"))
		finalScript.WriteString("\n") // Add a newline after the last command block
	}

	return finalScript.String(), nil
}
