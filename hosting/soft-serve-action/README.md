# Soft Serve Action

A GitHub/GitLab CI action that mirrors repositories to a Soft Serve instance.
Useful for keeping a local Soft Serve mirror in sync with upstream GitHub/GitLab
repositories.

## Source

This is a git subtree of:
https://github.com/charmbracelet/soft-serve-action

To pull the subtree:

```bash
git subtree add \
  --prefix hosting/soft-serve-action \
  https://github.com/charmbracelet/soft-serve-action.git main \
  --squash
```

## GitLab CI usage

Mirror a repository to the local Soft Serve instance on every push:

```yaml
mirror-to-soft-serve:
  stage: deploy
  image: alpine/git:latest
  variables:
    SOFT_SERVE_HOST: git.gitlab.local
    SOFT_SERVE_PORT: "23231"
  script:
    - eval $(ssh-agent -s)
    - echo "$SOFT_SERVE_SSH_KEY" | ssh-add -
    - git remote add soft-serve ssh://git@${SOFT_SERVE_HOST}:${SOFT_SERVE_PORT}/${CI_PROJECT_NAME} || true
    - git push soft-serve HEAD:main --force
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
```

## GitHub Actions usage

```yaml
- uses: charmbracelet/soft-serve-action@v1
  with:
    host: git.gitlab.local
    port: 23231
    key: ${{ secrets.SOFT_SERVE_SSH_KEY }}
```

## Mirror all submodules

To mirror all submodule upstreams to Soft Serve for air-gapped operation:

```bash
git submodule foreach '
  REPO_NAME=$(basename $displaypath)
  git remote add soft-serve ssh://git@git.gitlab.local:23231/${REPO_NAME} 2>/dev/null || true
  git push soft-serve HEAD:main --force
'
```
