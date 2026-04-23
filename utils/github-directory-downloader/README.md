# github-directory-downloader

A CLI tool for downloading a specific subdirectory from a GitHub repository
without cloning the entire repo. Uses the GitHub Contents API to fetch only
the files needed.

## Source

This is a git subtree of:
https://github.com/nicholasgasior/github-directory-downloader

To pull the subtree:

```bash
git subtree add \
  --prefix utils/github-directory-downloader \
  https://github.com/nicholasgasior/github-directory-downloader.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd utils/github-directory-downloader
go build -o bin/github-directory-downloader .
sudo install -m 755 bin/github-directory-downloader /usr/local/bin/
```

## Role in gitlab-enhanced

Used during bootstrap and CI to fetch specific components from upstream
repositories without pulling the full repo. Particularly useful for:

- Fetching Ansible roles from large monorepos (e.g., a single role from
  a community collection)
- Downloading Terraform modules from upstream repos
- Fetching specific config file templates from reference implementations

## Usage

```bash
# Download a specific directory from a GitHub repo
github-directory-downloader \
  --owner gitpod-io \
  --repo gitpod \
  --path components/supervisor/cmd \
  --output /tmp/supervisor-cmd

# With authentication (avoids rate limiting)
GITHUB_TOKEN=ghp_xxx github-directory-downloader \
  --owner moby \
  --repo buildkit \
  --path examples \
  --output /tmp/buildkit-examples
```

## Alternative: git sparse-checkout

For larger directories or when you need git history, use sparse-checkout instead:

```bash
git clone --filter=blob:none --sparse https://github.com/gitpod-io/gitpod.git
cd gitpod
git sparse-checkout set components/supervisor
```

`github-directory-downloader` is faster for small, well-defined file sets where
git history is not needed.
