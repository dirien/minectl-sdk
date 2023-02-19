[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/dirien/minectl-sdk/badge)](https://api.securityscorecards.dev/projects/github.com/dirien/minectl-sdk)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=dirien_minectl-sdk&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=dirien_minectl-sdk)
[![Go Reference](https://pkg.go.dev/badge/github.com/dirien/minectl-sdk.svg)](https://pkg.go.dev/github.com/dirien/minectl-sdk)
[![Go Report Card](https://goreportcard.com/badge/github.com/dirien/minectl-sdk)](https://goreportcard.com/report/github.com/dirien/minectl-sdk)

# The minectl-sdk

SDK for every minectl product

## Breaking changes

### v0.8.0

- Rename `Linode` to `Akamai Connected Cloud` and all related files. See this [blog post](https://www.linode.com/blog/linode/a-bold-new-approach-to-the-cloud/) for more information.

### v0.4.0

- Rename of the field `keyFolder` to `publickeyfile` in the [SSH Struct](/model/model.go)
- Add new field `publickey` in the [SSH Struct](/model/model.go)
- Both fields are optional, but at least one of them must be set.

## Todo

- [ ] Add tests
