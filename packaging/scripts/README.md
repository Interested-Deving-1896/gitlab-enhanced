# Packaging Scripts

Lifecycle scripts invoked by the package manager during install and removal.

| Script           | When                  | Purpose                                      |
|------------------|-----------------------|----------------------------------------------|
| `postinstall.sh` | After package install | Create runtime dirs, write default local.yaml |
| `preremove.sh`   | Before package remove | Stop services, remove PATH hint              |

## Notes

- `postinstall.sh` is idempotent — safe to run multiple times
- Neither script deletes user data (`/var/lib/gitlab-enhanced`)
- Data cleanup is left to the operator to avoid accidental loss

## Building

These scripts are referenced by `packaging/config/nfpm.yaml` and are embedded
into the `.deb`/`.rpm` package by nfpm at build time.
