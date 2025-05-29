package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		"ln -sf /usr/share/zoneinfo/America/New_York /etc/localtime",
		"echo 'LANG=en_US.UTF-8' > /etc/locale.conf",
		"useradd",
		"firewall-offline-cmd",
		"systemctl enable nginx",
		"systemctl disable telnet",
		"dnf install -y nginx",
		"dnf clean all",
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

	// Check that "dnf clean all" is the last command
	trimmedOutput := strings.TrimSpace(output)
	assert.True(t, strings.HasSuffix(trimmedOutput, "dnf clean all"), "Script should end with 'dnf clean all'")
}

func TestApplyCommand(t *testing.T) {
	// Create a temporary directory for test artifacts
	tmpDir, err := os.MkdirTemp("", "imagecfg-test-*")
	require.NoError(t, err, "Failed to create temporary directory")
	defer os.RemoveAll(tmpDir)

	// Determine absolute project root for volume mounting
	_, currentFilePath, _, ok := runtime.Caller(0)
	require.True(t, ok, "Failed to get current file path via runtime.Caller")
	projectRoot := filepath.Join(filepath.Dir(currentFilePath), "..", "..")
	projectRootAbs, err := filepath.Abs(projectRoot)
	require.NoError(t, err, "Failed to get absolute path for project root")

	// Build imagecfg using ubi10/go-toolset container
	buildCmd := exec.Command("podman", "run", "--rm",
		"-v", tmpDir+":/build_output:z", // Mount tmpDir to /build_output in the container
		"-v", projectRootAbs+":/src", // Mount absolute project root to /src in the container
		"-w", "/src/cmd/imagecfg", // Set working directory in the container
		"-e", "CGO_ENABLED=0", // Pass CGO_ENABLED=0 into the container
		"registry.access.redhat.com/ubi10/go-toolset",
		"go", "build", "-o", "/build_output/imagecfg")

	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build imagecfg using podman: %s", out)

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
