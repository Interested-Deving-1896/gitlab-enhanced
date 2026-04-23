# Blacksmith Runner Integration

[Blacksmith](https://www.blacksmith.sh/) provides fast, ephemeral GitHub Actions runners
backed by hardware-accelerated VMs. This directory contains configuration for registering
a Blacksmith runner pool against the gitlab-enhanced CI pipeline.

## When to use

Use Blacksmith runners when:
- You need faster CI than GitLab.com shared runners (especially for Go/Rust builds)
- You want native ARM64 builds without QEMU emulation
- Your pipeline is on GitHub Actions (Blacksmith is GitHub-native)

For GitLab CI, use the Incus custom executor instead (`runtime/incus/runner/`).

## Setup

1. Create a Blacksmith account at https://app.blacksmith.sh
2. Install the Blacksmith GitHub App on your repository
3. Replace `runs-on: ubuntu-latest` with `runs-on: blacksmith-2vcpu-ubuntu-2204`

## Available runner sizes

| Runner                          | vCPU | RAM  |
|---------------------------------|------|------|
| blacksmith-2vcpu-ubuntu-2204    | 2    | 8 GB |
| blacksmith-4vcpu-ubuntu-2204    | 4    | 16 GB|
| blacksmith-8vcpu-ubuntu-2204    | 8    | 32 GB|
| blacksmith-2vcpu-ubuntu-2204-arm| 2    | 8 GB (ARM64) |

## Example workflow snippet

```yaml
jobs:
  build:
    runs-on: blacksmith-4vcpu-ubuntu-2204
    steps:
      - uses: actions/checkout@v4
      - uses: useblacksmith/setup-go@v6
        with:
          go-version: '1.24'
      - run: go build ./...
```

## Cache integration

Blacksmith provides a drop-in cache replacement that is significantly faster than
`actions/cache` for large dependency trees:

```yaml
      - uses: useblacksmith/cache@v5
        with:
          path: ~/go/pkg/mod
          key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
```
