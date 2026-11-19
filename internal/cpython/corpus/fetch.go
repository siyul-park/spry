package corpus

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// FetchOption configures Fetch.
type FetchOption func(*fetcher)

type fetcher struct {
	url string
	sum string
}

const (
	archiveURL    = "https://github.com/python/cpython/archive/refs/tags/v3.14.9.tar.gz"
	archiveSHA256 = "79bda7368e7ac571cc4a0e4048bcfbc6a2765fd037ee282ffdb3dba7e6224d97"
	marker        = ".spry-cpython"
)

// ErrChecksum reports a downloaded archive that does not match its pinned hash.
var ErrChecksum = errors.New("corpus: cpython archive checksum mismatch")

var fetchedDirs = []string{"Lib", "Grammar", "Parser"}

// Fetch downloads the pinned CPython source archive, verifies its SHA-256 and
// extracts Lib/, Grammar/ and Parser/ (without the top directory) into dir. It
// extracts into a sibling temporary directory and renames it into place, and
// does nothing when dir already holds the same archive.
func Fetch(ctx context.Context, dir string, opts ...FetchOption) error {
	f := &fetcher{url: archiveURL, sum: archiveSHA256}
	for _, opt := range opts {
		opt(f)
	}
	if got, err := os.ReadFile(filepath.Join(dir, marker)); err == nil && string(got) == f.sum {
		return nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".fetch-*")
	if err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, "archive.tar.gz")
	if err := f.download(ctx, archive); err != nil {
		return err
	}
	out := filepath.Join(tmp, "src")
	if err := extract(archive, out); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, marker), []byte(f.sum), 0o644); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	if err := os.Rename(out, dir); err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	return nil
}

// WithArchive replaces the pinned archive URL and hex SHA-256, for tests.
func WithArchive(url, sum string) FetchOption {
	return func(f *fetcher) { f.url, f.sum = url, sum }
}

func (f *fetcher) download(ctx context.Context, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("corpus: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("corpus: download %s: %s", f.url, resp.Status)
	}
	file, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, h), resp.Body); err != nil {
		return fmt.Errorf("corpus: download: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.sum {
		return fmt.Errorf("%w: got %s, want %s", ErrChecksum, got, f.sum)
	}
	return nil
}

func extract(archive, dst string) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("corpus: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("corpus: extract: %w", err)
		}
		_, rel, _ := strings.Cut(path.Clean(hdr.Name), "/")
		top, _, _ := strings.Cut(rel, "/")
		if !slices.Contains(fetchedDirs, top) || !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			err = writeFile(target, tr)
		}
		if err != nil {
			return fmt.Errorf("corpus: extract %s: %w", rel, err)
		}
	}
}

func writeFile(target string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
