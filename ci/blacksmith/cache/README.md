# Blacksmith Cache

Blacksmith's cache backend (`useblacksmith/cache`) is a drop-in replacement for
`actions/cache` that stores artifacts on Blacksmith's infrastructure co-located
with the runner VMs, giving significantly lower latency than GitHub's cache service.

## Usage

Replace `actions/cache` with `useblacksmith/cache` — the API is identical:

```yaml
- uses: useblacksmith/cache@v5
  with:
    path: |
      ~/go/pkg/mod
      ~/.cache/go-build
    key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
    restore-keys: |
      ${{ runner.os }}-go-
```

## Supported cache paths for this project

| Cache key pattern              | Path                        | Purpose              |
|--------------------------------|-----------------------------|----------------------|
| `go-${{ hashFiles(go.sum) }}`  | `~/go/pkg/mod`, `~/.cache/go-build` | Go module + build cache |
| `node-${{ hashFiles(yarn.lock) }}` | `node_modules`          | Node dependencies    |
| `rust-${{ hashFiles(Cargo.lock) }}` | `~/.cargo/registry`   | Rust crate cache     |

No additional configuration is required beyond switching the action name.
