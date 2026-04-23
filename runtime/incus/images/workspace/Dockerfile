# gitlab-enhanced workspace image
# Base image for developer workspaces launched by the Incus environment backend.
#
# Built on top of Ubuntu 24.04 with a full developer toolchain pre-installed.
# The image is published to the local Incus image store as:
#   gitlab-enhanced/workspace-full:latest
#
# Build:
#   bash runtime/incus/images/workspace/build.sh
#
# What's included:
#   - Go 1.24, Node.js LTS, Python 3, Ruby, Rust
#   - Git, git-lfs, gh (GitHub CLI), glab (GitLab CLI)
#   - OpenVSCode Server (browser-based VS Code)
#   - Docker CLI (connects to BuildKit via socket, no daemon)
#   - Common dev tools: jq, yq, curl, make, direnv, pre-commit

FROM ubuntu:24.04

ARG GO_VERSION=1.24.2
ARG NODE_VERSION=20
ARG OPENVSCODE_VERSION=1.89.1
ARG TARGETARCH=amd64

ENV DEBIAN_FRONTEND=noninteractive \
    GOPATH=/go \
    GOROOT=/usr/local/go \
    PATH=/usr/local/go/bin:/go/bin:/home/user/.cargo/bin:/usr/local/bin:$PATH \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8

# ── System packages ───────────────────────────────────────────────────────────
RUN apt-get update -qq && apt-get install -y -qq \
    # Core
    curl wget ca-certificates gnupg lsb-release \
    # Build tools
    build-essential pkg-config cmake \
    # Version control
    git git-lfs \
    # Shell tools
    bash zsh fish \
    jq unzip zip \
    direnv \
    # Python
    python3 python3-pip python3-venv pipx \
    # Ruby
    ruby ruby-dev \
    # Misc
    locales sudo openssh-client \
    && locale-gen en_US.UTF-8 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# ── Go ────────────────────────────────────────────────────────────────────────
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz \
    && mkdir -p /go/bin /go/pkg /go/src

# ── Node.js ───────────────────────────────────────────────────────────────────
RUN curl -fsSL https://deb.nodesource.com/setup_${NODE_VERSION}.x | bash - \
    && apt-get install -y -qq nodejs \
    && apt-get clean && rm -rf /var/lib/apt/lists/* \
    && npm install -g yarn pnpm

# ── Rust ──────────────────────────────────────────────────────────────────────
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
    | sh -s -- -y --no-modify-path --profile minimal \
    && /root/.cargo/bin/rustup component add rust-analyzer

# ── yq ───────────────────────────────────────────────────────────────────────
RUN YQ_VERSION=v4.44.1 && \
    curl -fsSL "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_${TARGETARCH}" \
    -o /usr/local/bin/yq && chmod +x /usr/local/bin/yq

# ── glab (GitLab CLI) ─────────────────────────────────────────────────────────
RUN GLAB_VERSION=1.46.1 && \
    curl -fsSL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_linux_${TARGETARCH}.deb" \
    -o /tmp/glab.deb && dpkg -i /tmp/glab.deb && rm /tmp/glab.deb

# ── pre-commit ────────────────────────────────────────────────────────────────
RUN pipx install pre-commit && \
    ln -sf /root/.local/bin/pre-commit /usr/local/bin/pre-commit

# ── OpenVSCode Server ─────────────────────────────────────────────────────────
RUN OPENVSCODE_TARBALL="openvscode-server-v${OPENVSCODE_VERSION}-linux-${TARGETARCH}.tar.gz" && \
    curl -fsSL "https://github.com/gitpod-io/openvscode-server/releases/download/openvscode-server-v${OPENVSCODE_VERSION}/${OPENVSCODE_TARBALL}" \
    | tar -xz -C /opt && \
    mv "/opt/openvscode-server-v${OPENVSCODE_VERSION}-linux-${TARGETARCH}" /opt/openvscode-server && \
    ln -sf /opt/openvscode-server/bin/openvscode-server /usr/local/bin/openvscode-server

# ── Non-root user ─────────────────────────────────────────────────────────────
RUN useradd -m -s /bin/bash -G sudo user && \
    echo 'user ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers && \
    mkdir -p /workspace && chown user:user /workspace /go

USER user
WORKDIR /workspace

# ── User-level Go tools ───────────────────────────────────────────────────────
RUN go install golang.org/x/tools/gopls@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest && \
    go install gotest.tools/gotestsum@v1.12.0

EXPOSE 3000

# OpenVSCode Server on port 3000, no auth (auth handled by Incus network isolation)
CMD ["/opt/openvscode-server/bin/openvscode-server", \
     "--host", "0.0.0.0", \
     "--port", "3000", \
     "--without-connection-token", \
     "--default-folder", "/workspace"]
