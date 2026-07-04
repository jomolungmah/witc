// Package index persists a witc summary to disk as a cached JSON index and
// detects when that cache is stale, so the expensive type-checked build is paid
// once per source change rather than on every query.
package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jomolungmah/witc/internal/scanner"
)

// Dir is the per-project directory, relative to the analyzed root, that holds
// the cached index and its metadata. Consumers should add it to .gitignore.
const Dir = ".witc"

const (
	indexName = "index.json"
	metaName  = "index.meta"
)

// Path returns the location of the cached JSON index under root.
func Path(root string) string { return filepath.Join(root, Dir, indexName) }

func metaPath(root string) string { return filepath.Join(root, Dir, metaName) }

// ComputeKey derives a content key for the scanned files from each file's path,
// size, and modification time, plus a caller-supplied salt (the JSON schema
// version). It changes whenever a file is added, removed, edited, or resized —
// capturing uncommitted changes a git revision would miss — and whenever the
// schema changes, so an upgraded binary never serves an old-schema cache.
func ComputeKey(root string, files []scanner.File, salt string) (string, error) {
	sorted := append([]scanner.File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	h := sha256.New()
	fmt.Fprintf(h, "salt:%s\n", salt)
	for _, f := range sorted {
		fi, err := os.Stat(filepath.Join(root, f.Path))
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", f.Path, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", filepath.ToSlash(f.Path), fi.Size(), fi.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Write persists the JSON index and records key as the cache metadata, creating
// the .witc directory if needed.
func Write(root, jsonData, key string) error {
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(Path(root), []byte(jsonData), 0o644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	if err := os.WriteFile(metaPath(root), []byte(key), 0o644); err != nil {
		return fmt.Errorf("write index meta: %w", err)
	}
	return nil
}

// Fresh reports whether a persisted index exists and was built from the given
// cache key (i.e. the source has not changed since).
func Fresh(root, key string) bool {
	if _, err := os.Stat(Path(root)); err != nil {
		return false
	}
	data, err := os.ReadFile(metaPath(root))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == key
}

// Load reads the raw persisted index JSON.
func Load(root string) ([]byte, error) {
	return os.ReadFile(Path(root))
}
