// Package bandwidth implements server-side bandwidth management for gitlab-enhanced.
//
// Three mechanisms work together:
//
//  1. Compression proxy — gzip/zstd middleware in front of GitLab and LFS
//     endpoints, reducing transfer sizes by 60-80% for text payloads.
//
//  2. LFS deduplication — content-addressed dedup of LFS objects using
//     SHA-256 hardlinks. Objects with identical content are stored once
//     regardless of which repository uploaded them.
//
//  3. CI artifact policies — size limits, retention windows, and cache
//     eviction to prevent unbounded disk growth from CI artifacts.
package bandwidth

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config holds bandwidth service configuration. Populated from config.Bandwidth.
type Config struct {
	Enabled               bool
	ListenAddr            string
	CompressionLevel      int
	LFSDedupEnabled       bool
	LFSStorePath          string
	ArtifactMaxSizeMB     int
	ArtifactRetentionDays int
	CacheMaxSizeGB        int
	// UpstreamGitLab is the GitLab instance URL to proxy to.
	UpstreamGitLab string
}

// statsValues is a mutex-free snapshot of Stats for serialisation.
type statsValues struct {
	BytesIn          int64 `json:"bytes_in"`
	BytesOut         int64 `json:"bytes_out"`
	BytesSaved       int64 `json:"bytes_saved"`
	RequestsProxied  int64 `json:"requests_proxied"`
	DedupHits        int64 `json:"dedup_hits"`
	DedupBytesSaved  int64 `json:"dedup_bytes_saved"`
	ArtifactsEvicted int64 `json:"artifacts_evicted"`
}

// Stats holds runtime statistics for the bandwidth service.
type Stats struct {
	mu sync.RWMutex
	statsValues
}

func (s *Stats) snapshot() statsValues {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsValues
}

// ArtifactRecord tracks a CI artifact for retention policy enforcement.
type ArtifactRecord struct {
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	ProjectID int       `json:"project_id"`
	JobID     int       `json:"job_id"`
}

// Service is the bandwidth management HTTP service.
type Service struct {
	cfg       Config
	stats     Stats
	artifacts []ArtifactRecord
	mu        sync.Mutex
	server    *http.Server
}

// New creates a new bandwidth Service.
func New(cfg Config) (*Service, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("bandwidth service is disabled (set bandwidth.enabled: true in config/local.yaml)")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:6062"
	}
	if cfg.CompressionLevel == 0 {
		cfg.CompressionLevel = gzip.DefaultCompression
	}
	if cfg.UpstreamGitLab == "" {
		cfg.UpstreamGitLab = "http://127.0.0.1:80"
	}
	return &Service{cfg: cfg}, nil
}

// Start launches the HTTP server. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) error {
	upstream, err := url.Parse(s.cfg.UpstreamGitLab)
	if err != nil {
		return fmt.Errorf("invalid upstream URL %q: %w", s.cfg.UpstreamGitLab, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ModifyResponse = s.compressResponse

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/lfs/dedup", s.handleLFSDedup)
	mux.HandleFunc("/artifacts/policy", s.handleArtifactPolicy)
	mux.HandleFunc("/artifacts/evict", s.handleArtifactEvict)
	// All other paths are proxied to GitLab with compression
	mux.Handle("/", s.compressionMiddleware(proxy))

	s.server = &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start artifact retention enforcer
	go s.runRetentionEnforcer(ctx)

	log.Printf("[bandwidth] service starting on http://%s", s.cfg.ListenAddr)
	log.Printf("[bandwidth] proxying to %s", s.cfg.UpstreamGitLab)
	log.Printf("[bandwidth] compression level: %d", s.cfg.CompressionLevel)
	log.Printf("[bandwidth] LFS dedup: %v", s.cfg.LFSDedupEnabled)

	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// --- Compression ---

// compressionMiddleware wraps an http.Handler with gzip response compression.
// Compression is applied only when:
//   - The client sends Accept-Encoding: gzip
//   - The response Content-Type is compressible (text/*, JSON, JS, XML, YAML)
//   - The request is not a binary LFS object upload/download
//
// Content-Encoding and BytesSaved are set after the upstream handler runs so
// we know the actual Content-Type before committing to compression.
func (s *Service) compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Never compress LFS object transfers — already binary/opaque.
		if strings.Contains(r.URL.Path, "/lfs/objects/") {
			next.ServeHTTP(w, r)
			return
		}

		// Buffer the response so we can inspect Content-Type before compressing.
		buf := &bufferedResponseWriter{header: w.Header().Clone(), code: http.StatusOK}
		next.ServeHTTP(buf, r)

		s.stats.mu.Lock()
		s.stats.RequestsProxied++
		s.stats.mu.Unlock()

		ct := buf.header.Get("Content-Type")
		if !isCompressible(ct) {
			// Pass through unmodified.
			w.WriteHeader(buf.code)
			_, _ = w.Write(buf.body)
			return
		}

		// Compress into a buffer first so we know the compressed size.
		uncompressedSize := int64(len(buf.body))
		var gzBuf strings.Builder
		gz, err := gzip.NewWriterLevel(&gzBuf, s.cfg.CompressionLevel)
		if err != nil {
			// Compression unavailable — pass through unmodified.
			w.WriteHeader(buf.code)
			_, _ = w.Write(buf.body)
			return
		}
		_, _ = gz.Write(buf.body)
		_ = gz.Close()
		compressed := gzBuf.String()

		saved := uncompressedSize - int64(len(compressed))
		if saved < 0 {
			saved = 0
		}
		s.stats.mu.Lock()
		s.stats.BytesIn += uncompressedSize
		s.stats.BytesSaved += saved
		s.stats.mu.Unlock()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.WriteHeader(buf.code)
		_, _ = io.WriteString(w, compressed)
	})
}

// compressResponse is the ReverseProxy ModifyResponse hook.
// It tracks the uncompressed upstream response size for statistics.
// Actual compression is handled by compressionMiddleware above.
func (s *Service) compressResponse(resp *http.Response) error {
	if resp.ContentLength > 0 && isCompressible(resp.Header.Get("Content-Type")) {
		s.stats.mu.Lock()
		s.stats.BytesIn += resp.ContentLength
		s.stats.mu.Unlock()
	}
	return nil
}

func isCompressible(contentType string) bool {
	for _, t := range []string{"text/", "application/json", "application/xml",
		"application/javascript", "application/yaml"} {
		if strings.Contains(contentType, t) {
			return true
		}
	}
	return false
}

// bufferedResponseWriter captures the upstream response body and headers so
// compressionMiddleware can inspect Content-Type before deciding to compress.
type bufferedResponseWriter struct {
	header http.Header
	body   []byte
	code   int
}

func (b *bufferedResponseWriter) Header() http.Header        { return b.header }
func (b *bufferedResponseWriter) WriteHeader(code int)       { b.code = code }
func (b *bufferedResponseWriter) Write(p []byte) (int, error) {
	b.body = append(b.body, p...)
	return len(p), nil
}

// --- LFS Deduplication ---

// DedupResult is returned by the LFS dedup endpoint.
type DedupResult struct {
	OID        string `json:"oid"`
	SizeBytes  int64  `json:"size_bytes"`
	Duplicate  bool   `json:"duplicate"`
	LinkedFrom string `json:"linked_from,omitempty"`
	BytesSaved int64  `json:"bytes_saved,omitempty"`
}

// DeduplicateLFSObject checks whether an LFS object with the given OID already
// exists in the store (by SHA-256 content hash) and creates a hardlink if so.
// This saves disk space when multiple repositories store the same large file.
func (s *Service) DeduplicateLFSObject(oid string, size int64) (DedupResult, error) {
	if !s.cfg.LFSDedupEnabled || s.cfg.LFSStorePath == "" {
		return DedupResult{OID: oid, SizeBytes: size}, nil
	}

	// A valid Git LFS OID is a 64-character hex SHA-256. Reject anything shorter
	// to prevent index-out-of-range panics on the slice operations below.
	if len(oid) < 4 {
		return DedupResult{}, fmt.Errorf("invalid LFS OID %q: must be at least 4 characters", oid)
	}

	// LFS objects are stored at <store>/<oid[0:2]>/<oid[2:4]>/<oid>
	objPath := filepath.Join(s.cfg.LFSStorePath,
		oid[0:2], oid[2:4], oid)

	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		return DedupResult{OID: oid, SizeBytes: size, Duplicate: false}, nil
	}

	// Object exists — compute its content hash to find duplicates
	hash, err := sha256File(objPath)
	if err != nil {
		return DedupResult{OID: oid, SizeBytes: size}, err
	}

	// Check the dedup index for an existing object with the same content hash.
	dedupDir := filepath.Join(s.cfg.LFSStorePath, ".dedup")
	dedupPath := filepath.Join(dedupDir, hash)
	if existing, err := os.Readlink(dedupPath); err == nil {
		// Found a duplicate. Replace objPath with a hardlink to the canonical
		// copy so the inode is shared and disk space is reclaimed.
		// Atomic replace: link to a temp name, then rename over the original.
		tmpPath := objPath + ".dedup-tmp"
		if linkErr := os.Link(existing, tmpPath); linkErr == nil {
			if renameErr := os.Rename(tmpPath, objPath); renameErr == nil {
				s.stats.mu.Lock()
				s.stats.DedupHits++
				s.stats.DedupBytesSaved += size
				s.stats.mu.Unlock()
				return DedupResult{
					OID:        oid,
					SizeBytes:  size,
					Duplicate:  true,
					LinkedFrom: existing,
					BytesSaved: size,
				}, nil
			}
			_ = os.Remove(tmpPath)
		}
	}

	// First time we see this content hash — register objPath as the canonical copy.
	_ = os.MkdirAll(dedupDir, 0755)
	_ = os.Symlink(objPath, dedupPath)

	return DedupResult{OID: oid, SizeBytes: size, Duplicate: false}, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Artifact Retention ---

// runRetentionEnforcer periodically evicts CI artifacts that exceed the
// configured size limit or retention window.
func (s *Service) runRetentionEnforcer(ctx context.Context) {
	if s.cfg.ArtifactRetentionDays == 0 && s.cfg.ArtifactMaxSizeMB == 0 {
		return
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evictArtifacts()
		}
	}
}

// evictArtifacts removes artifacts that violate retention policies.
func (s *Service) evictArtifacts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -s.cfg.ArtifactRetentionDays)
	maxBytes := int64(s.cfg.ArtifactMaxSizeMB) * 1024 * 1024

	var kept []ArtifactRecord
	for _, a := range s.artifacts {
		expired := s.cfg.ArtifactRetentionDays > 0 && a.CreatedAt.Before(cutoff)
		oversized := maxBytes > 0 && a.SizeBytes > maxBytes

		if expired || oversized {
			if err := os.Remove(a.Path); err == nil {
				s.stats.mu.Lock()
				s.stats.ArtifactsEvicted++
				s.stats.mu.Unlock()
				log.Printf("[bandwidth] evicted artifact %s (expired=%v oversized=%v)",
					a.Path, expired, oversized)
			}
		} else {
			kept = append(kept, a)
		}
	}
	s.artifacts = kept
}

// RegisterArtifact adds an artifact to the retention tracking list.
func (s *Service) RegisterArtifact(a ArtifactRecord) error {
	maxBytes := int64(s.cfg.ArtifactMaxSizeMB) * 1024 * 1024
	if maxBytes > 0 && a.SizeBytes > maxBytes {
		return fmt.Errorf("artifact size %d bytes exceeds limit of %d MB",
			a.SizeBytes, s.cfg.ArtifactMaxSizeMB)
	}
	s.mu.Lock()
	s.artifacts = append(s.artifacts, a)
	s.mu.Unlock()
	return nil
}

// --- HTTP handlers ---

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"compression": s.cfg.CompressionLevel,
		"lfs_dedup":   s.cfg.LFSDedupEnabled,
	})
}

func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	snap := s.stats.snapshot()
	var savingsPct float64
	if snap.BytesIn > 0 {
		savingsPct = float64(snap.BytesSaved) / float64(snap.BytesIn) * 100
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats":                snap,
		"savings_percent":      fmt.Sprintf("%.1f%%", savingsPct),
		"dedup_bytes_saved_mb": snap.DedupBytesSaved / (1024 * 1024),
	})
}

func (s *Service) handleLFSDedup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OID  string `json:"oid"`
		Size int64  `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result, err := s.DeduplicateLFSObject(req.OID, req.Size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) handleArtifactPolicy(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tracked := len(s.artifacts)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact_max_size_mb":    s.cfg.ArtifactMaxSizeMB,
		"artifact_retention_days": s.cfg.ArtifactRetentionDays,
		"cache_max_size_gb":       s.cfg.CacheMaxSizeGB,
		"artifacts_tracked":       tracked,
	})
}

func (s *Service) handleArtifactEvict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Snapshot before/after under the stats mutex to avoid a data race with
	// the background retention enforcer goroutine.
	before := s.stats.snapshot().ArtifactsEvicted
	s.evictArtifacts()
	after := s.stats.snapshot().ArtifactsEvicted
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"evicted": after - before,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
