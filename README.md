# imagecfg 🛠️

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`imagecfg` is a tool that enables using OSBuild blueprints with bootc container images. It provides feature parity between image-builder (osbuild-composer) and bootc (image mode) images by allowing the same blueprint to be used for both.

> ⚠️ **Note**: This project is currently experimental and does not yet support all blueprint fields. Use with caution in production environments.

## Quick Example

Create a Containerfile:

```dockerfile
FROM quay.io/fedora/fedora-bootc:42

COPY config.toml /usr/lib/bootc-image-builder/config.toml
RUN imagecfg apply
```

With a configuration file (`config.toml`):

```toml
[customizations]
hostname = "my-server"

[customizations.timezone]
timezone = "America/New_York"
ntpservers = ["pool.ntp.org"]

[customizations.locale]
languages = ["en_US.UTF-8"]
keyboard = "us"

[[customizations.user]]
name = "admin"
password = "$6$xyz..."  # Hashed password
groups = ["wheel"]
key = "ssh-rsa AAAA..."

[customizations.firewall]
ports = ["80/tcp", "443/tcp"]
services = { enabled = ["http", "https"] }

[customizations.services]
enabled = ["nginx"]
disabled = ["telnet"]

[[packages]]
name = "nginx"
```

Build the container:

```bash
podman build -t my-custom-bootc-image .
```

## Commands

### `imagecfg apply [blueprint.toml]`
Applies an OSBuild blueprint by configuring the system according to the blueprint specifications. If no blueprint path is provided, defaults to `/usr/lib/bootc-image-builder/config.toml`.

### `imagecfg bash [blueprint.toml]`
Debug tool that translates an OSBuild blueprint to a bash script for inspection. Uses the same default path as `apply`.

## Configuration Reference

The configuration file uses TOML format and supports the following customizations:

### System Settings

```toml
[customizations]
hostname = "my-server"

[customizations.timezone]
timezone = "America/New_York"
ntpservers = ["pool.ntp.org"]

[customizations.locale]
languages = ["en_US.UTF-8"]
keyboard = "us"
```

### Users and Groups

```toml
[[customizations.group]]
name = "developers"
gid = 1000

[[customizations.user]]
name = "admin"
password = "$6$xyz..."  # Hashed password
home = "/home/admin"    # Optional
shell = "/bin/bash"     # Optional
groups = ["wheel"]      # Additional groups
key = "ssh-rsa AAAA..." # SSH public key
uid = 1000             # Optional
gid = 1000             # Optional
```

### Firewall

```toml
[customizations.firewall]
ports = ["80/tcp", "443/tcp"]
services = { enabled = ["http", "https"] }
```

### Services

```toml
[customizations.services]
enabled = ["nginx", "postgresql"]
disabled = ["telnet"]
masked = ["rpcbind"]
```

### Packages

```toml
[[packages]]
name = "nginx"

[[packages]]
name = "postgresql-server"
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
