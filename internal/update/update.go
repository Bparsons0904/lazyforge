// Package update checks for a newer release and swaps the running binary for it; it imports nothing from internal/.
package update

import (
	"archive/tar"
	"archive/zip"
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
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// DefaultBaseURL is the Forgejo host that publishes lazyforge releases.
const DefaultBaseURL = "https://git.bobparsons.dev"

// The GitHub mirror of the Forgejo releases, used when Forgejo is unreachable.
const (
	DefaultGitHubAPIURL = "https://api.github.com"
	DefaultGitHubURL    = "https://github.com"
)

const repoPath = "deadstyle/lazyforge"

// errUnreachable marks a source that could not be reached: a transport error, a timeout or a 5xx status.
var errUnreachable = errors.New("unreachable")

// Source is one host's release layout; Name labels it in errors.
type Source struct {
	Name         string // "forgejo" or "github"
	LatestURL    string // JSON with tag_name and body
	DownloadBase string // assets live at DownloadBase + "/" + tag + "/" + asset
}

// DownloadURL returns the URL of one asset of tag on this source.
func (s Source) DownloadURL(tag, asset string) string {
	return s.DownloadBase + "/" + tag + "/" + asset
}

// Forgejo is the release layout of the Forgejo host at baseURL.
func Forgejo(baseURL string) Source {
	return Source{
		Name:         "forgejo",
		LatestURL:    baseURL + "/api/v1/repos/" + repoPath + "/releases/latest",
		DownloadBase: baseURL + "/" + repoPath + "/releases/download",
	}
}

// GitHub is the release layout of the GitHub mirror, with its API at apiURL and its web host at webURL.
func GitHub(apiURL, webURL string) Source {
	return Source{
		Name:         "github",
		LatestURL:    apiURL + "/repos/Bparsons0904/lazyforge/releases/latest",
		DownloadBase: webURL + "/Bparsons0904/lazyforge/releases/download",
	}
}

// Release is the latest published release.
type Release struct {
	Tag     string
	Summary string // first non-empty line of the release body
	Source  Source // where Latest found it; Apply downloads from here
}

// Checker talks to the release hosts.
type Checker struct {
	Sources      []Source     // tried in order by Latest
	Client       *http.Client // must be non-nil; Timeout applies per request, so per source
	GOOS, GOARCH string
}

// Latest returns the latest release from the first source that answers; a source that
// answers with a non-5xx error stops the search, since another host would only disagree.
func (c Checker) Latest(ctx context.Context) (Release, error) {
	if len(c.Sources) == 0 {
		return Release{}, errors.New("no release sources configured")
	}
	var errs []error
	for _, s := range c.Sources {
		rel, err := c.latest(ctx, s)
		if err == nil {
			return rel, nil
		}
		errs = append(errs, err)
		if !errors.Is(err, errUnreachable) {
			return Release{}, errors.Join(errs...)
		}
	}
	return Release{}, errors.Join(errs...)
}

func (c Checker) latest(ctx context.Context, s Source) (Release, error) {
	body, err := c.get(ctx, s.LatestURL)
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w", s.Name, err)
	}
	defer discard(body.Close)
	// Read the whole body here so a cut-off response is classed as unreachable, not as a bad document.
	data, err := io.ReadAll(body)
	if err != nil {
		return Release{}, fmt.Errorf("%s: %w: read %s: %w", s.Name, errUnreachable, s.LatestURL, err)
	}
	var r struct {
		Tag  string `json:"tag_name"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Release{}, fmt.Errorf("%s: decode latest release: %w", s.Name, err)
	}
	if r.Tag == "" {
		return Release{}, fmt.Errorf("%s: latest release has no tag", s.Name)
	}
	return Release{Tag: r.Tag, Summary: firstLine(r.Body), Source: s}, nil
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
		return nil, fmt.Errorf("%w: get %s: %w", errUnreachable, url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, fmt.Errorf("%w: get %s: status %d", errUnreachable, url, resp.StatusCode)
		}
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
// Nothing at exe changes unless every earlier step succeeded. The archive and checksums both come
// from rel.Source, and Apply never falls back to another source.
func (c Checker) Apply(ctx context.Context, rel Release, exe string) (err error) {
	if rel.Source.DownloadBase == "" {
		return errors.New("release has no download source")
	}
	dir := filepath.Dir(exe)
	archive := fmt.Sprintf("lazyforge_%s_%s_%s.tar.gz", rel.Tag, c.GOOS, c.GOARCH)
	extract := extractTarGz
	if c.GOOS == "windows" {
		archive = fmt.Sprintf("lazyforge_%s_%s_%s.zip", rel.Tag, c.GOOS, c.GOARCH)
		extract = extractZip
	}

	arc, err := c.download(ctx, rel.Source.DownloadURL(rel.Tag, archive), dir)
	if err != nil {
		return err
	}
	defer discard(func() error { return os.Remove(arc) })
	sums, err := c.download(ctx, rel.Source.DownloadURL(rel.Tag, "checksums.txt"), dir)
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

// extractTarGz writes the archive's lazyforge entry to a 0755 temp file in dir and returns its path.
func extractTarGz(arc, dir string) (string, error) {
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
		return writeBinary(dir, tr)
	}
}

// extractZip writes the archive's lazyforge.exe entry to a 0755 temp file in dir and returns its path.
func extractZip(arc, dir string) (string, error) {
	zr, err := zip.OpenReader(arc)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer discard(zr.Close)
	for _, f := range zr.File {
		if !f.Mode().IsRegular() || path.Clean(f.Name) != "lazyforge.exe" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("read archive: %w", err)
		}
		defer discard(rc.Close)
		return writeBinary(dir, rc)
	}
	return "", errors.New("archive has no lazyforge binary")
}

// writeBinary copies src to a 0755 temp file in dir and returns its path; a failed copy leaves nothing behind.
func writeBinary(dir string, src io.Reader) (string, error) {
	out, err := os.CreateTemp(dir, ".lazyforge-new-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, src)
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

// Rollback restores exe+".old" over exe.
func Rollback(exe string) error {
	return os.Rename(exe+".old", exe)
}

// CleanupOld removes exe+".old" if present; errors are ignored.
func CleanupOld(exe string) {
	_ = os.Remove(exe + ".old")
}

func discard(f func() error) { _ = f() }
