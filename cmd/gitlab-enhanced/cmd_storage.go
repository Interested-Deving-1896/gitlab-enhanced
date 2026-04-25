package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

func newStorageCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Inspect and interact with the configured storage backend",
		Long: `Inspect and interact with the configured storage backend.

Backends (set via storage.backend in config):
  local   — local filesystem (default)
  ipfs    — IPFS node via Kubo HTTP API
  cloud   — cloud provider (aws|gcs|azure|minio|ceph|r2)
  chain   — local → IPFS → cloud fallback`,
	}
	cmd.AddCommand(
		newStorageStatusCmd(cfgRoot),
		newStorageListCmd(cfgRoot),
		newStoragePutCmd(cfgRoot),
		newStorageGetCmd(cfgRoot),
		newStorageDeleteCmd(cfgRoot),
		newStorageStatCmd(cfgRoot),
	)
	return cmd
}

func newStorageStatusCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether the storage backend is reachable",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageStatus(*cfgRoot)
		},
	}
}

func runStorageStatus(root string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	printSection("Storage backend")
	printInfo(fmt.Sprintf("backend: %s", cfg.Storage.Backend))
	printInfo(fmt.Sprintf("name:    %s", b.Name()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if b.Available(ctx) {
		printOK("storage backend is reachable")
	} else {
		printFail("storage backend is not reachable")
		return fmt.Errorf("storage backend %q is not available", b.Name())
	}
	return nil
}

func newStorageListCmd(cfgRoot *string) *cobra.Command {
	var prefix string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List objects in the storage backend",
		Example: `  gitlab-enhanced storage list
  gitlab-enhanced storage list --prefix lfs/objects/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageList(*cfgRoot, prefix)
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "filter by key prefix")
	return cmd
}

func runStorageList(root, prefix string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	objects, err := b.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("listing objects: %w", err)
	}

	if len(objects) == 0 {
		printInfo("no objects found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tSIZE\tMODIFIED")
	for _, obj := range objects {
		mod := ""
		if !obj.ModTime.IsZero() {
			mod = obj.ModTime.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%d\t%s\n", obj.Key, obj.Size, mod)
	}
	return w.Flush()
}

func newStoragePutCmd(cfgRoot *string) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "put <key>",
		Short: "Upload a file to the storage backend",
		Example: `  gitlab-enhanced storage put lfs/objects/abc123 --file ./myfile
  cat myfile | gitlab-enhanced storage put lfs/objects/abc123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStoragePut(*cfgRoot, args[0], file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "path to file to upload (reads stdin if not set)")
	return cmd
}

func runStoragePut(root, key, filePath string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	var r io.Reader
	var size int64 = -1

	if filePath != "" {
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("opening %s: %w", filePath, err)
		}
		defer f.Close()
		if info, err := f.Stat(); err == nil {
			size = info.Size()
		}
		r = f
	} else {
		r = os.Stdin
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := b.Put(ctx, key, r, size); err != nil {
		return fmt.Errorf("put %q: %w", key, err)
	}
	printOK(fmt.Sprintf("stored %q", key))
	return nil
}

func newStorageGetCmd(cfgRoot *string) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Download an object from the storage backend",
		Example: `  gitlab-enhanced storage get lfs/objects/abc123 --output ./myfile
  gitlab-enhanced storage get lfs/objects/abc123 > myfile`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageGet(*cfgRoot, args[0], output)
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "write to file instead of stdout")
	return cmd
}

func runStorageGet(root, key, outputPath string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rc, _, err := b.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("get %q: %w", key, err)
	}
	defer rc.Close()

	var w io.Writer
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating %s: %w", outputPath, err)
		}
		defer f.Close()
		w = f
	} else {
		w = os.Stdout
	}

	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func newStorageDeleteCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <key>",
		Aliases: []string{"rm"},
		Short:   "Delete an object from the storage backend",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageDelete(*cfgRoot, args[0])
		},
	}
}

func runStorageDelete(root, key string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := b.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete %q: %w", key, err)
	}
	printOK(fmt.Sprintf("deleted %q", key))
	return nil
}

func newStorageStatCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stat <key>",
		Short: "Show metadata for an object in the storage backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStorageStat(*cfgRoot, args[0])
		},
	}
}

func runStorageStat(root, key string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising storage backend: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	obj, err := b.Stat(ctx, key)
	if err != nil {
		return fmt.Errorf("stat %q: %w", key, err)
	}

	printSection(fmt.Sprintf("Object: %s", obj.Key))
	printInfo(fmt.Sprintf("size:         %d bytes", obj.Size))
	if obj.ContentType != "" {
		printInfo(fmt.Sprintf("content-type: %s", obj.ContentType))
	}
	if !obj.ModTime.IsZero() {
		printInfo(fmt.Sprintf("modified:     %s", obj.ModTime.Format(time.RFC3339)))
	}
	return nil
}
