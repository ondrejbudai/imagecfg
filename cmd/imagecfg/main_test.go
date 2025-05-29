package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
		"hostnamectl set-hostname my-server",
		"timedatectl set-timezone America/New_York",
		"localectl set-locale LANG=en_US.UTF-8",
		"useradd",
		"firewall-offline-cmd",
		"systemctl enable nginx",
		"systemctl disable telnet",
		"dnf install -y nginx",
	}

	for _, part := range expectedParts {
		if !strings.Contains(output, part) {
			t.Errorf("Expected script to contain %q, but it didn't", part)
		}
	}

	// Also test that the script contains specific firewall rules from our test config
	firewallRules := []string{
		"--add-port=80/tcp",
		"--add-port=443/tcp",
		"--add-service=http",
		"--add-service=https",
	}

	for _, rule := range firewallRules {
		if !strings.Contains(output, rule) {
			t.Errorf("Expected script to contain firewall rule %q, but it didn't", rule)
		}
	}
}
