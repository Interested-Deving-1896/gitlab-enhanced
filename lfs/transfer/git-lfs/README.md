# git-lfs

The official Git LFS client. Included as a subtree so the exact version used
in workspace images and CI containers can be pinned and built from source.

## Source

This is a git subtree of:
https://github.com/git-lfs/git-lfs

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/transfer/git-lfs \
  https://github.com/git-lfs/git-lfs.git main \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/transfer/git-lfs
make
sudo make install
```

## Version pinning

The workspace Dockerfile installs git-lfs from the Ubuntu package repository.
To pin to a specific version built from this subtree, replace the apt install
step with:

```dockerfile
COPY --from=builder /usr/local/bin/git-lfs /usr/local/bin/git-lfs
```

where `builder` is a stage that runs `make` in this directory.

## Usage

Standard git-lfs usage — no gitlab-enhanced-specific configuration required:

```bash
git lfs install
git lfs track "*.bin" "*.tar.gz" "*.zip"
git add .gitattributes
git commit -m "track large files with LFS"
git push
```
