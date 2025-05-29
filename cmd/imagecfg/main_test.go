package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashCommand(t *testing.T) {
	// Save the original stdout and create a pipe to capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create a new command instance
	cmd := &cobra.Command{}
	args := []string{"../../test/config.toml"}

	// Run the bash command
	bashCmd.Run(cmd, args)

	// Restore stdout and get the output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Test for expected content in the generated script
	expectedParts := []string{
		"#!/bin/bash",
		"echo 'my-server' > /etc/hostname",
		"timedatectl set-timezone America/New_York",
		"localectl set-locale LANG=en_US.UTF-8",
		"useradd",
		"firewall-offline-cmd",
		"systemctl enable nginx",
		"systemctl disable telnet",
		"dnf install -y nginx",
	}

	for _, part := range expectedParts {
		assert.Contains(t, output, part, "Script should contain %q", part)
	}

	// Also test that the script contains specific firewall rules from our test config
	firewallRules := []string{
		"--add-port=80/tcp",
		"--add-port=443/tcp",
		"--add-service=http",
		"--add-service=https",
	}

	for _, rule := range firewallRules {
		assert.Contains(t, output, rule, "Script should contain firewall rule %q", rule)
	}
}

func TestApplyCommand(t *testing.T) {
	// Create a temporary directory for test artifacts
	tmpDir, err := os.MkdirTemp("", "imagecfg-test-*")
	require.NoError(t, err, "Failed to create temporary directory")
	defer os.RemoveAll(tmpDir)

	// Build imagecfg in the temporary directory, cross-compiled for Linux
	binaryPath := filepath.Join(tmpDir, "imagecfg")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/imagecfg")
	buildCmd.Dir = "../.."

	// poor man's cross-compilation
	// TODO: use a red hat golang image
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build imagecfg: %s", out)

	// Copy config.toml to temp dir
	copyCmd := exec.Command("cp", "../../test/config.toml", filepath.Join(tmpDir, "config.toml"))
	out, err = copyCmd.CombinedOutput()
	require.NoError(t, err, "Failed to copy config.toml: %s", out)

	// Create a Containerfile
	containerfile := `FROM fedora-bootc:42
COPY imagecfg /usr/local/bin/
COPY config.toml /usr/lib/bootc-image-builder/config.toml`

	containerfilePath := filepath.Join(tmpDir, "Containerfile")
	err = os.WriteFile(containerfilePath, []byte(containerfile), 0644)
	require.NoError(t, err, "Failed to write Containerfile")

	// Build the container image
	buildContainerCmd := exec.Command("podman", "build", "-t", "imagecfg-test", tmpDir)
	out, err = buildContainerCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build container: %s", out)

	// Run the container with apply command
	runCmd := exec.Command("podman", "run", "--rm", "imagecfg-test", "imagecfg", "apply")
	out, err = runCmd.CombinedOutput()
	require.NoError(t, err, "Failed to run apply command: %s", out)
}
