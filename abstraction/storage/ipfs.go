package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// IPFSBackend stores objects on an IPFS node via the Kubo HTTP RPC API.
// Objects are stored as IPFS files using the MFS (Mutable File System) API,
// which provides a stable path-based addressing layer over content-addressed CIDs.
//
// API docs: https://docs.ipfs.tech/reference/kubo/rpc/
type IPFSBackend struct {
	nodeURL string
	client  *http.Client
}

func NewIPFSBackend(nodeURL string) *IPFSBackend {
	if nodeURL == "" {
		nodeURL = "http://127.0.0.1:5001"
	}
	return &IPFSBackend{
		nodeURL: strings.TrimRight(nodeURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (b *IPFSBackend) Name() string { return "ipfs:" + b.nodeURL }

func (b *IPFSBackend) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.nodeURL+"/api/v0/version", nil)
	if err != nil {
		return false
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Put stores data at the given MFS path using /api/v0/files/write.
func (b *IPFSBackend) Put(ctx context.Context, key string, r io.Reader, _ int64) error {
	mfsPath := mfsKey(key)

	// Ensure parent directory exists
	dir := mfsPath[:strings.LastIndex(mfsPath, "/")]
	if dir != "" {
		mkdirURL := fmt.Sprintf("%s/api/v0/files/mkdir?arg=%s&parents=true", b.nodeURL, dir)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, mkdirURL, nil)
		resp, err := b.client.Do(req)
		if err != nil {
			return fmt.Errorf("ipfs mkdir %q: %w", dir, err)
		}
		resp.Body.Close()
	}

	// Write file via multipart upload
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fw, err := mw.CreateFormFile("file", key)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(fw, r); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	writeURL := fmt.Sprintf("%s/api/v0/files/write?arg=%s&create=true&truncate=true",
		b.nodeURL, mfsPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, writeURL, pr)
	if err != nil {
		return fmt.Errorf("ipfs write %q: %w", key, err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("ipfs write %q: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ipfs write %q: status %d: %s", key, resp.StatusCode, body)
	}
	return nil
}

// Get retrieves data from the MFS path using /api/v0/files/read.
func (b *IPFSBackend) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	stat, err := b.Stat(ctx, key)
	if err != nil {
		return nil, nil, err
	}

	readURL := fmt.Sprintf("%s/api/v0/files/read?arg=%s", b.nodeURL, mfsKey(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, readURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("ipfs read %q: %w", key, err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("ipfs read %q: %w", key, err)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		resp.Body.Close()
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("ipfs read %q: status %d: %s", key, resp.StatusCode, body)
	}
	return resp.Body, stat, nil
}

// Delete removes the MFS path using /api/v0/files/rm.
func (b *IPFSBackend) Delete(ctx context.Context, key string) error {
	rmURL := fmt.Sprintf("%s/api/v0/files/rm?arg=%s&recursive=true", b.nodeURL, mfsKey(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rmURL, nil)
	if err != nil {
		return fmt.Errorf("ipfs rm %q: %w", key, err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("ipfs rm %q: %w", key, err)
	}
	defer resp.Body.Close()
	// 500 with "file does not exist" is a no-op
	if resp.StatusCode == http.StatusInternalServerError {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ipfs rm %q: status %d: %s", key, resp.StatusCode, body)
	}
	return nil
}

// Stat returns metadata using /api/v0/files/stat.
func (b *IPFSBackend) Stat(ctx context.Context, key string) (*Object, error) {
	statURL := fmt.Sprintf("%s/api/v0/files/stat?arg=%s", b.nodeURL, mfsKey(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, statURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ipfs stat %q: %w", key, err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipfs stat %q: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ipfs stat %q: status %d: %s", key, resp.StatusCode, body)
	}
	var result struct {
		Size uint64 `json:"Size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ipfs stat %q: decoding response: %w", key, err)
	}
	return &Object{
		Key:  key,
		Size: int64(result.Size),
	}, nil
}

// List returns all MFS entries under the given prefix using /api/v0/files/ls.
func (b *IPFSBackend) List(ctx context.Context, prefix string) ([]Object, error) {
	lsURL := fmt.Sprintf("%s/api/v0/files/ls?arg=%s&long=true", b.nodeURL, mfsKey(prefix))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ipfs ls %q: %w", prefix, err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipfs ls %q: %w", prefix, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		return nil, nil // directory doesn't exist yet
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ipfs ls %q: status %d: %s", prefix, resp.StatusCode, body)
	}
	var result struct {
		Entries []struct {
			Name string `json:"Name"`
			Size uint64 `json:"Size"`
		} `json:"Entries"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("ipfs ls %q: decoding response: %w", prefix, err)
	}
	objects := make([]Object, 0, len(result.Entries))
	for _, e := range result.Entries {
		fullKey := strings.TrimPrefix(prefix+"/"+e.Name, "/")
		objects = append(objects, Object{
			Key:  fullKey,
			Size: int64(e.Size),
		})
	}
	return objects, nil
}

// mfsKey converts a storage key to an absolute MFS path.
func mfsKey(key string) string {
	if strings.HasPrefix(key, "/") {
		return "/gitlab-enhanced" + key
	}
	return "/gitlab-enhanced/" + key
}

var _ Backend = (*IPFSBackend)(nil)
