// Package update checks for a newer release and swaps the running binary for it; it imports nothing from internal/.
package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// DefaultBaseURL is the Forgejo host that publishes lazyforge releases.
const DefaultBaseURL = "https://git.bobparsons.dev"

const repoPath = "deadstyle/lazyforge"

// Release is the latest published release.
type Release struct {
	Tag     string
	Summary string // first non-empty line of the release body
}

// Checker talks to the release host.
type Checker struct {
	BaseURL      string
	Client       *http.Client // must be non-nil; the caller sets Timeout
	GOOS, GOARCH string
}

// Latest fetches the latest release; a non-200 response is an error.
func (c Checker) Latest(ctx context.Context) (Release, error) {
	body, err := c.get(ctx, c.BaseURL+"/api/v1/repos/"+repoPath+"/releases/latest")
	if err != nil {
		return Release{}, err
	}
	defer discard(body.Close)
	var r struct {
		Tag  string `json:"tag_name"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(body).Decode(&r); err != nil {
		return Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if r.Tag == "" {
		return Release{}, errors.New("latest release has no tag")
	}
	return Release{Tag: r.Tag, Summary: firstLine(r.Body)}, nil
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func (c Checker) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

// Newer reports whether latest is a newer plain release than current.
// Invalid versions (dev, git describe) and prerelease latests never offer an update.
func Newer(current, latest string) bool {
	if !semver.IsValid(current) || !semver.IsValid(latest) || semver.Prerelease(latest) != "" {
		return false
	}
	return semver.Compare(latest, current) > 0
}

// Ask prompts on out and reads one line from in; empty input at the newline means yes, EOF means no.
func Ask(in io.Reader, out io.Writer, current string, rel Release) bool {
	if rel.Summary != "" {
		_, _ = fmt.Fprintln(out, rel.Summary)
	}
	_, _ = fmt.Fprintf(out, "lazyforge %s is available (you have %s). Update now? [Y/n] ", rel.Tag, current)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}

// Apply downloads and verifies the release archive, then swaps it in for exe via rename.
// Nothing at exe changes unless every earlier step succeeded.
func (c Checker) Apply(ctx context.Context, rel Release, exe string) (err error) {
	dir := filepath.Dir(exe)
	archive := fmt.Sprintf("lazyforge_%s_%s_%s.tar.gz", rel.Tag, c.GOOS, c.GOARCH)
	base := c.BaseURL + "/" + repoPath + "/releases/download/" + rel.Tag + "/"

	arc, err := c.download(ctx, base+archive, dir)
	if err != nil {
		return err
	}
	defer discard(func() error { return os.Remove(arc) })
	sums, err := c.download(ctx, base+"checksums.txt", dir)
	if err != nil {
		return err
	}
	defer discard(func() error { return os.Remove(sums) })

	if err := verify(arc, sums, archive); err != nil {
		return err
	}
	newBin, err := extract(arc, dir)
	if err != nil {
		return err
	}
	defer discard(func() error { return os.Remove(newBin) }) // gone after a successful rename; cleans up on failure

	if err := os.Rename(exe, exe+".old"); err != nil {
		return fmt.Errorf("move old binary aside: %w", err)
	}
	if err := os.Rename(newBin, exe); err != nil {
		if rbErr := os.Rename(exe+".old", exe); rbErr != nil {
			return errors.Join(fmt.Errorf("install new binary: %w", err), rbErr)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

// download saves url to a temp file in dir and returns its path.
func (c Checker) download(ctx context.Context, url, dir string) (string, error) {
	body, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer discard(body.Close)
	f, err := os.CreateTemp(dir, ".lazyforge-update-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	return f.Name(), nil
}

// verify checks arc's SHA-256 against the line for name in the sha256sum-format file sums.
func verify(arc, sums, name string) error {
	data, err := os.ReadFile(sums)
	if err != nil {
		return err
	}
	var want string
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum for %s", name)
	}
	f, err := os.Open(arc)
	if err != nil {
		return err
	}
	defer discard(f.Close)
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

// extract writes the archive's lazyforge entry to a 0755 temp file in dir and returns its path.
func extract(arc, dir string) (string, error) {
	f, err := os.Open(arc)
	if err != nil {
		return "", err
	}
	defer discard(f.Close)
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("archive has no lazyforge binary")
		}
		if err != nil {
			return "", fmt.Errorf("read archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Clean(h.Name) != "lazyforge" {
			continue
		}
		out, err := os.CreateTemp(dir, ".lazyforge-new-*")
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(out.Name(), 0o755)
		}
		if err != nil {
			_ = os.Remove(out.Name())
			return "", fmt.Errorf("extract binary: %w", err)
		}
		return out.Name(), nil
	}
}

// Rollback restores exe+".old" over exe.
func Rollback(exe string) error {
	return os.Rename(exe+".old", exe)
}

// CleanupOld removes exe+".old" if present; errors are ignored.
func CleanupOld(exe string) {
	_ = os.Remove(exe + ".old")
}

func discard(f func() error) { _ = f() }
