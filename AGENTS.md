<!-- FOR AI AGENTS - Human readability is a side effect, not a goal -->
<!-- Managed by agent: keep sections and order; edit content, not structure -->
<!-- Last updated: 2026-03-21 -->

# AGENTS.md

**Precedence:** the **closest `AGENTS.md`** to the files you're changing wins. Root holds global defaults only.

## Commands
> Source: go.mod, .golangci.yaml, CLAUDE.md

| Task | Command | ~Time |
|------|---------|-------|
| Typecheck | `go build ./...` | ~10s |
| Lint | `golangci-lint run` | ~15s |
| Format | `gofumpt -w .` | ~2s |
| Test (single) | `go test -v -race -run TestName ./package/...` | ~2s |
| Test (all) | `go test -v -race ./...` | ~30s |
| Build | `go build ./...` | ~10s |
| Tidy deps | `go mod tidy` | ~5s |

> If commands fail, verify against go.mod or ask user to update.

## Workflow
1. **Before coding**: Read nearest `AGENTS.md` + check Golden Samples for the area you're touching
2. **After each change**: Run the smallest relevant check (lint → typecheck → single test)
3. **Before committing**: Run full test suite if changes affect >2 files or touch shared code
4. **Before claiming done**: Run verification and **show output as evidence** — never say "try again" or "should work now" without proof

## File Map
```
.
├── automation/          # Core interface: Automation + ServerArgs, ResourceResults
│   └── automation.go
├── cloud/               # Cloud provider implementations (each implements automation.Automation)
│   ├── cloud.go         # Provider name mapping + SSH key helper
│   ├── akamai/          # Akamai Connected Cloud (Linode)
│   ├── aws/             # Amazon Web Services
│   ├── azure/           # Microsoft Azure
│   ├── civo/            # Civo
│   ├── do/              # DigitalOcean
│   ├── exoscale/        # Exoscale
│   ├── fuga/            # Fuga Cloud (OpenStack-based)
│   ├── gce/             # Google Compute Engine
│   ├── hetzner/         # Hetzner Cloud
│   ├── multipass/       # Ubuntu Multipass (local)
│   ├── oci/             # Oracle Cloud Infrastructure
│   ├── openstack/       # Generic OpenStack
│   ├── ovh/             # OVHcloud
│   ├── scaleway/        # Scaleway
│   ├── vexxhost/        # VEXXHOST (OpenStack-based)
│   └── vultr/           # Vultr
├── common/              # Shared utilities (StringPtr, server name helpers, color output)
│   └── common.go
├── model/               # Data models: MinecraftResource, server spec, SSH, Java settings
│   └── model.go
├── template/            # Go templates for cloud-init/bash provisioning (//go:embed)
│   ├── template.go
│   └── template_test.go
├── update/              # Remote server ops via SSH (update, file transfer, commands)
│   └── server.go
├── .golangci.yaml       # Linter config (tagliatelle, forbidigo, gosec, etc.)
├── go.mod               # Go 1.24.9
└── CONTRIBUTING.md      # DCO sign-off required
```

## Golden Samples (follow these patterns)

| For | Reference | Key patterns |
|-----|-----------|--------------|
| Cloud provider | `cloud/hetzner/hetzner.go` | Implements `automation.Automation`, uses native SDK |
| Test | `template/template_test.go` | Table-driven tests with golden files |
| Interface | `automation/automation.go` | Central interface definition |
| Model | `model/model.go` | Getter-based config model |

## Utilities (check before creating new)

| Need | Use | Location |
|------|-----|----------|
| Create server name with tags | `CreateServerNameWithTags` | `common/` |
| Extract fields from server name | `ExtractFieldsFromServername` | `common/` |
| Colored output | `Green` | `common/` |
| String pointer | `StringPtr` | `common/` |
| SSH public key from args | `GetSSHPublicKey` | `cloud/cloud.go` |
| Provider name lookup | `GetCloudProviderFullName` / `GetCloudProviderCode` | `cloud/cloud.go` |

## Heuristics (quick decisions)

| When | Do |
|------|-----|
| Adding a cloud provider | Create dir under `cloud/`, implement `automation.Automation`, register in `cloud/cloud.go` + `model/model.go` |
| JSON struct tags | Use `snake_case` (enforced by `tagliatelle` linter) |
| YAML struct tags | Use `camelCase` (enforced by `tagliatelle` linter) |
| Need `ioutil.*` | Use `os` / `io` equivalents (forbidden by `forbidigo`) |
| Embedding files | Use `//go:embed` directive (see `template/`) |
| SSH operations | Use `github.com/melbahja/goph` |
| Template functions | Use `github.com/Masterminds/sprig/v3` |
| Adding dependency | Ask first — we minimize deps |
| Unsure about pattern | Check Golden Samples above |

## Boundaries

### Always Do
- Sign-off commits with `git commit -s` (DCO compliance)
- Run pre-commit checks before committing
- Add tests for new code paths
- Use conventional commit format: `type(scope): subject`
- **Show test output as evidence before claiming work is complete**
- Handle all errors (enforced by `errcheck`)
- Use `gofumpt` formatting (not just `gofmt`)

### Ask First
- Adding new dependencies
- Modifying CI/CD configuration
- Changing the `automation.Automation` interface
- Adding new cloud providers
- Running full e2e test suites
- Repo-wide refactoring or rewrites

### Never Do
- Commit secrets, credentials, or sensitive data
- Use `ioutil.*` functions (use `os`/`io` instead)
- Push directly to main/master branch
- Skip error handling
- Add `//nolint` without justification
- Modify generated files

## Module Boundaries

> Cloud provider packages (`cloud/*`) must not import each other. Shared logic → `common/` or `cloud/cloud.go`.

## Terminology

| Term | Means |
|------|-------|
| `Automation` | Interface all cloud providers implement |
| `MinecraftResource` | Top-level config model for a Minecraft server |
| `ServerArgs` | Arguments for provider operations |
| `DCO` | Developer Certificate of Origin — requires `git commit -s` |

## Index of scoped AGENTS.md

No scoped files — this is a flat SDK. All conventions apply globally.

## When instructions conflict
The nearest `AGENTS.md` wins. Explicit user prompts override files.
