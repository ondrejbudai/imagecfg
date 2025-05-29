package main

import (
	"fmt"
	"strings"

	"github.com/osbuild/blueprint/pkg/blueprint"
)

// generateHostnameCmd generates the bash command for setting the hostname.
func generateHostnameCmd(bp *blueprint.Blueprint) (string, error) {
	hostname := bp.Customizations.GetHostname()
	if hostname == nil || *hostname == "" {
		return "", nil // No hostname specified
	}
	cmd := fmt.Sprintf("echo '%s' > /etc/hostname", *hostname)
	return cmd, nil
}

// generateTimezoneCmd generates bash commands for setting the timezone.
func generateTimezoneCmd(bp *blueprint.Blueprint) (string, error) {
	timezone, ntpservers := bp.Customizations.GetTimezoneSettings()

	var cmds []string

	if timezone != nil && *timezone != "" {
		cmds = append(cmds, fmt.Sprintf("timedatectl set-timezone %s", *timezone))
	}

	if len(ntpservers) > 0 {
		for _, ntp := range ntpservers {
			cmds = append(cmds,
				fmt.Sprintf("sed -i '/^server %s /d' /etc/chrony.conf", ntp),
				fmt.Sprintf("echo 'server %s iburst' >> /etc/chrony.conf", ntp),
			)
		}
	}
	return strings.Join(cmds, " && "), nil
}

// generateLocaleCmd generates bash commands for locale and keyboard settings.
func generateLocaleCmd(bp *blueprint.Blueprint) (string, error) {
	locale, keyboardLayout := bp.Customizations.GetPrimaryLocale()

	var cmds []string

	if locale != nil && *locale != "" {
		cmds = append(cmds, fmt.Sprintf("localectl set-locale LANG=%s", *locale))
	}

	if keyboardLayout != nil && *keyboardLayout != "" {
		cmds = append(cmds, fmt.Sprintf("localectl set-keymap %s", *keyboardLayout))
	}

	if len(cmds) == 0 {
		return "", nil
	}

	return strings.Join(cmds, " && "), nil
}

// generateGroupsBlockCmd generates a block of bash commands for creating groups.
func generateGroupsBlockCmd(bp *blueprint.Blueprint) (string, error) {
	groups := bp.Customizations.GetGroups()
	if len(groups) == 0 {
		return "", nil
	}
	var groupCmdLines []string

	for _, group := range groups {
		groupaddBase := "groupadd"
		if group.GID != nil {
			groupaddBase += fmt.Sprintf(" --gid %d", *group.GID)
		}
		groupaddCmd := fmt.Sprintf("%s %s", groupaddBase, group.Name)
		safeAddCmd := fmt.Sprintf("(getent group %s > /dev/null || %s)", group.Name, groupaddCmd)
		groupCmdLines = append(groupCmdLines, safeAddCmd)
	}
	return strings.Join(groupCmdLines, "\n"), nil
}

// generateUsersBlockCmd generates a block of bash commands for creating/configuring users.
func generateUsersBlockCmd(bp *blueprint.Blueprint) (string, error) {
	users := bp.Customizations.GetUsers()
	if len(users) == 0 {
		return "", nil
	}

	var userBlockLines []string

	for _, user := range users {
		var singleUserCmds []string

		useraddCmdParts := []string{"useradd"}
		if user.Home != nil && *user.Home != "" {
			useraddCmdParts = append(useraddCmdParts, "-d", *user.Home, "-m")
		} else {
			useraddCmdParts = append(useraddCmdParts, "-m")
		}
		if user.Shell != nil && *user.Shell != "" {
			useraddCmdParts = append(useraddCmdParts, "-s", *user.Shell)
		}
		if user.UID != nil {
			useraddCmdParts = append(useraddCmdParts, "-u", fmt.Sprintf("%d", *user.UID))
		}
		if user.GID != nil {
			useraddCmdParts = append(useraddCmdParts, "-g", fmt.Sprintf("%d", *user.GID))
		}
		useraddFullCmd := strings.Join(useraddCmdParts, " ")
		singleUserCmds = append(singleUserCmds, fmt.Sprintf("(getent passwd %s > /dev/null || %s)", user.Name, useraddFullCmd))

		// --- Secondary Groups ---
		if len(user.Groups) > 0 {
			singleUserCmds = append(singleUserCmds, fmt.Sprintf("usermod -aG %s %s", strings.Join(user.Groups, ","), user.Name))
		}

		// --- Password ---
		if user.Password != nil && *user.Password != "" {
			singleUserCmds = append(singleUserCmds, fmt.Sprintf("echo '%s:%s' | chpasswd -e", user.Name, *user.Password))
		}

		// --- SSH Key ---
		if user.Key != nil && *user.Key != "" {
			homeDir := "/home/" + user.Name // Default home directory
			if user.Home != nil && *user.Home != "" {
				homeDir = *user.Home // Use specified home directory
			}
			// Ensure correct permissions and ownership for SSH key
			sshCmd := fmt.Sprintf("mkdir -p %s/.ssh && echo '%s' | tee %s/.ssh/authorized_keys > /dev/null && chmod 700 %s/.ssh && chmod 600 %s/.ssh/authorized_keys && chown -R %s:%s %s/.ssh",
				homeDir, *user.Key, homeDir, homeDir, homeDir, user.Name, user.Name, homeDir) // Assumes primary group name is same as user name for chown
			singleUserCmds = append(singleUserCmds, sshCmd)
		}

		// Join all commands for this single user with '&&'
		userBlockLines = append(userBlockLines, strings.Join(singleUserCmds, " && "))
	}
	// Join command lines for all users with newlines
	return strings.Join(userBlockLines, "\n"), nil
}

// generateFirewallCmd generates bash commands for firewall configuration.
func generateFirewallCmd(bp *blueprint.Blueprint) (string, error) {
	fwCustom := bp.Customizations.GetFirewall()
	if fwCustom == nil {
		return "", nil // No firewall customization
	}
	var fwRuleCmds []string // Holds individual firewall-cmd calls

	if len(fwCustom.Ports) > 0 {
		for _, port := range fwCustom.Ports {
			fwRuleCmds = append(fwRuleCmds, fmt.Sprintf("firewall-offline-cmd --add-port=%s", port))
		}
	}
	if fwCustom.Services != nil && len(fwCustom.Services.Enabled) > 0 {
		for _, service := range fwCustom.Services.Enabled {
			fwRuleCmds = append(fwRuleCmds, fmt.Sprintf("firewall-offline-cmd --add-service=%s", service))
		}
	}

	if len(fwRuleCmds) == 0 {
		return "", nil // No firewall rules to apply
	}

	return strings.Join(fwRuleCmds, " && "), nil
}

// generateServicesCmd generates bash commands for enabling/disabling/masking system services.
func generateServicesCmd(bp *blueprint.Blueprint) (string, error) {
	svcCustom := bp.Customizations.GetServices()
	if svcCustom == nil {
		return "", nil // No service customization
	}
	var serviceManagementCmds []string

	if len(svcCustom.Enabled) > 0 {
		for _, service := range svcCustom.Enabled {
			serviceManagementCmds = append(serviceManagementCmds, fmt.Sprintf("systemctl enable %s", service))
		}
	}
	if len(svcCustom.Disabled) > 0 {
		for _, service := range svcCustom.Disabled {
			serviceManagementCmds = append(serviceManagementCmds, fmt.Sprintf("systemctl disable %s", service))
		}
	}
	if len(svcCustom.Masked) > 0 {
		for _, service := range svcCustom.Masked {
			serviceManagementCmds = append(serviceManagementCmds, fmt.Sprintf("systemctl mask %s", service))
		}
	}

	if len(serviceManagementCmds) == 0 {
		return "", nil // No service actions to perform
	}

	return strings.Join(serviceManagementCmds, " && "), nil
}

// generatePackagesCmd generates the bash command for installing packages.
func generatePackagesCmd(bp *blueprint.Blueprint) (string, error) {
	packages := bp.GetPackages() // This method correctly gets all packages (from 'packages' and 'modules')
	if len(packages) == 0 {
		return "", nil // No packages to install
	}
	return fmt.Sprintf("dnf install -y %s", strings.Join(packages, " ")), nil
}
