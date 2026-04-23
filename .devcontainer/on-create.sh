#!/usr/bin/env bash
# Runs once when the devcontainer is first created.
set -euo pipefail

echo "→ Installing system dependencies"
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends \
  build-essential \
  cmake \
  pkg-config \
  libssl-dev \
  libffi-dev \
  yq \
  jq \
  shellcheck \
  yamllint \
  make \
  git-lfs \
  openssh-client \
  gnupg2 \
  ca-certificates \
  curl \
  wget \
  unzip

echo "→ Installing Go tools"
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/goimports@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

echo "→ Installing Python tools"
pip install --quiet --upgrade pip
pip install --quiet \
  ansible-lint \
  molecule \
  yamllint \
  pre-commit

echo "→ Installing Ruby tools (for omnibus)"
gem install --quiet bundler rake

echo "→ Initialising git submodules"
cd "$(git rev-parse --show-toplevel)"
git submodule update --init --recursive --depth 1 2>/dev/null || \
  echo "  ⚠ Some submodules could not be initialised (network or auth issue — run manually later)"

echo "→ Installing pre-commit hooks"
pre-commit install --install-hooks 2>/dev/null || true

echo "✅ on-create complete"
