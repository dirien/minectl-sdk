# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build all packages
go build ./...

# Run tests
go test ./...

# Run a single test
go test -run TestName ./package/...

# Tidy dependencies
go mod tidy

# Run linter (requires golangci-lint installed)
golangci-lint run
```

## Linting Configuration

The project uses golangci-lint with settings in `.golangci.yaml`:
- JSON tags use snake_case, YAML tags use camelCase (`tagliatelle` linter)
- `ioutil.*` functions are forbidden - use `os` and `io` packages instead
- Enabled linters include: `gofumpt`, `gocritic`, `gosec`, `errcheck`, `govet`

## Architecture Overview

This is a Go SDK for managing Minecraft servers across multiple cloud providers. The SDK follows a provider-agnostic pattern where each cloud provider implements a common interface.

### Core Interfaces

**`automation.Automation`** (`automation/automation.go`) - The central interface all cloud providers must implement:
- `CreateServer`, `DeleteServer`, `ListServer`, `UpdateServer`, `GetServer`, `UploadPlugin`

**`model.MinecraftResource`** (`model/model.go`) - The configuration model representing a Minecraft server specification with getters for server config, Minecraft edition, SSH settings, etc.

### Package Structure

- **`cloud/`** - Cloud provider implementations. Each subdirectory (akamai, aws, azure, civo, do, exoscale, fuga, gce, hetzner, multipass, oci, openstack, ovh, scaleway, vexxhost, vultr) contains a provider implementing `automation.Automation`
- **`automation/`** - Interface definitions and shared types (`ServerArgs`, `ResourceResults`)
- **`model/`** - Data models for Minecraft resource configuration (server spec, SSH, Java settings, etc.)
- **`template/`** - Go templates for cloud-init and bash scripts used during server provisioning. Templates are embedded via `//go:embed`
- **`update/`** - Remote server operations via SSH (update server, transfer files, execute commands)
- **`common/`** - Shared utilities

### Adding a New Cloud Provider

1. Create a new directory under `cloud/` with your provider name
2. Implement the `automation.Automation` interface
3. Add provider mapping in `cloud/cloud.go`
4. Add provider constant in `model/model.go`

### Key Dependencies

- Cloud SDKs: Each provider uses its native SDK (e.g., `aws-sdk-go-v2`, `azure-sdk-for-go`, `gophercloud/v2`, `scaleway-sdk-go`)
- SSH: `github.com/melbahja/goph` for remote command execution
- Templates: `github.com/Masterminds/sprig/v3` for template functions

## Commit Requirements

Sign-off commits with `git commit -s` for DCO compliance.
