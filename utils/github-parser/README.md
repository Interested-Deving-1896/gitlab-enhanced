# github-parser

A CLI tool for parsing and extracting structured data from GitHub API responses,
repository metadata, and GitHub Actions workflow files. Used in CI pipelines to
extract information from GitHub repositories that are mirrored into the
gitlab-enhanced monorepo.

## Source

This is a git subtree of:
https://github.com/nicholasgasior/github-parser

To pull the subtree:

```bash
git subtree add \
  --prefix utils/github-parser \
  https://github.com/nicholasgasior/github-parser.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd utils/github-parser
go build -o bin/github-parser .
sudo install -m 755 bin/github-parser /usr/local/bin/
```

## Role in gitlab-enhanced

github-parser is used in CI pipelines to:

1. Extract release metadata from GitHub API responses when mirroring upstream
   submodule releases
2. Parse GitHub Actions workflow files to identify equivalent GitLab CI jobs
3. Extract repository topics and descriptions for the Soft Serve mirror index

## Usage

```bash
# Parse a GitHub release API response
curl -s https://api.github.com/repos/moby/buildkit/releases/latest \
  | github-parser release --field tag_name

# Extract workflow job names from a GitHub Actions file
github-parser workflow --file .github/workflows/ci.yml --list-jobs

# Get repository metadata
github-parser repo --owner charmbracelet --name soft-serve --field description
```

## CI usage

```yaml
# In .gitlab-ci.yml — check for new upstream releases
check-upstream-versions:
  script:
    - BUILDKIT_LATEST=$(curl -s https://api.github.com/repos/moby/buildkit/releases/latest | github-parser release --field tag_name)
    - echo "Latest BuildKit: $BUILDKIT_LATEST"
    - grep -q "$BUILDKIT_LATEST" runtime/incus/cloud-init/buildkit.yaml || echo "UPDATE NEEDED"
```
