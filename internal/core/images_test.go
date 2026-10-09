package core_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

const (
	imageTimeout = 20 * time.Second
	waitTimeout  = 5 * time.Second
)

var imgRepo = domain.RepoRef{Owner: "o", Name: "r"}

// serveFunc answers one OpenAsset call.
type serveFunc func(ctx context.Context, u *url.URL) (io.ReadCloser, error)

// recorder is a Fake whose assets come from serve, and which logs every OpenAsset call.
type recorder struct {
	*forgetest.Fake
	serve serveFunc

	mu    sync.Mutex
	calls []assetCall
}

// assetCall is one OpenAsset call a recorder saw.
type assetCall struct {
	url string
	ctx context.Context
}

func (r *recorder) OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	r.mu.Lock()
	r.calls = append(r.calls, assetCall{url: u.String(), ctx: ctx})
	r.mu.Unlock()
	return r.serve(ctx, u)
}

func (r *recorder) callList() []assetCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]assetCall(nil), r.calls...)
}

// noAssets hides OpenAsset: embedding the interface promotes only the Forge methods.
type noAssets struct{ forge.Forge }

// serveBytes answers every URL with body.
func serveBytes(body []byte) serveFunc {
	return func(context.Context, *url.URL) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

// newImages returns a Service over a recorder on host https://h; a nil serve answers with DemoPNG.
func newImages(t *testing.T, serve serveFunc) (*core.Service, *recorder) {
	return newImagesOpts(t, serve, core.Options{})
}

func newImagesOpts(t *testing.T, serve serveFunc, opts core.Options) (*core.Service, *recorder) {
	t.Helper()
	if serve == nil {
		serve = serveBytes(forgetest.DemoPNG)
	}
	f := &recorder{
		Fake:  forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h", User: "bob", ChangeRequestTerm: "PR"}),
		serve: serve,
	}
	return core.New(f, opts), f
}

// blockingServe closes started on its call and holds the body until release is closed.
func blockingServe(started, release chan struct{}) serveFunc {
	return func(context.Context, *url.URL) (io.ReadCloser, error) {
		close(started)
		<-release
		return io.NopCloser(bytes.NewReader(forgetest.DemoPNG)), nil
	}
}

// waitForOpen fails the test unless ch, closed when OpenAsset starts, closes within waitTimeout.
func waitForOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		t.Fatalf("OpenAsset did not start within %s", waitTimeout)
	}
}

// recvErr returns the next result from ch, failing the test if none arrives within waitTimeout.
func recvErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(waitTimeout):
		t.Fatalf("%s did not return within %s", what, waitTimeout)
		return nil
	}
}

func imageNamed(i int) string { return fmt.Sprintf("a%d.png", i) }

// rgbaAt returns the colour at (x, y) after the conversion every test compares in.
func rgbaAt(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// near reports whether each channel of a is within tol of b's.
func near(a, b color.RGBA, tol int) bool {
	diff := func(x, y uint8) bool {
		d := int(x) - int(y)
		return d >= -tol && d <= tol
	}
	return diff(a.R, b.R) && diff(a.G, b.G) && diff(a.B, b.B) && diff(a.A, b.A)
}

func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, solidImage(w, h, c)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fixture reads a file from testdata. The JPEG and GIF bodies are fixtures, not encoder output, because
// importing their encoders here would register their decoders for the whole test binary.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pngHeaderOnly returns a PNG signature and a valid IHDR declaring w×h, with no pixel data after it.
func pngHeaderOnly(w, h uint32) []byte {
	var hdr bytes.Buffer
	_ = binary.Write(&hdr, binary.BigEndian, struct {
		W, H                                      uint32
		Depth, Color, Compression, Filter, Interl uint8
	}{w, h, 8, 6, 0, 0, 0})
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(hdr.Len()))
	b.WriteString("IHDR")
	b.Write(hdr.Bytes())
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), hdr.Bytes()...)))
	return b.Bytes()
}

// ctxReader blocks until its context ends, then reports a clean end of body, so only the check after the
// read can turn the expired deadline into the error.
type ctxReader struct{ ctx context.Context }

func (r ctxReader) Read([]byte) (int, error) {
	<-r.ctx.Done()
	return 0, io.EOF
}

func TestImageResolvesRawAgainstRepo(t *testing.T) {
	tests := []struct {
		raw, want string
	}{
		{"attachments/a.png", "https://h/o/r/attachments/a.png"},
		{"/attachments/a.png", "https://h/attachments/a.png"},
		{"https://h/x.png", "https://h/x.png"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			svc, f := newImages(t, nil)
			if _, err := svc.Image(t.Context(), imgRepo, tt.raw); err != nil {
				t.Fatal(err)
			}
			calls := f.callList()
			if len(calls) != 1 || calls[0].url != tt.want {
				t.Errorf("OpenAsset calls = %+v, want one call to %s", calls, tt.want)
			}
		})
	}
}

func TestImageRefusesBadRawWithoutFetch(t *testing.T) {
	tests := []struct {
		name, raw       string
		wantUnsupported bool
	}{
		{"other host", "https://other/x.png", true},
		{"scheme-relative other host", "//other/x.png", true},
		{"same host other port", "https://h:8443/x.png", true},
		{"other scheme", "http://h/x.png", true},
		{"data URL", "data:image/png;base64,iVBORw0KGgo=", true},
		{"userinfo", "https://u:p@h/x.png", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"unparsable", "%zz", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, f := newImages(t, nil)
			_, err := svc.Image(t.Context(), imgRepo, tt.raw)
			if err == nil {
				t.Fatal("Image returned no error")
			}
			if errors.Is(err, forge.ErrUnsupported) != tt.wantUnsupported {
				t.Errorf("Image error %v; wrapping ErrUnsupported = %v, want %v", err, !tt.wantUnsupported, tt.wantUnsupported)
			}
			img, ok, perr := svc.PeekImage(imgRepo, tt.raw)
			if img != nil || !ok || perr == nil {
				t.Errorf("PeekImage = (%v, %v, %v), want (nil, true, error)", img, ok, perr)
			}
			if errors.Is(perr, forge.ErrUnsupported) != tt.wantUnsupported {
				t.Errorf("PeekImage error %v does not match the Image refusal kind", perr)
			}
			if n := len(f.callList()); n != 0 {
				t.Errorf("OpenAsset was called %d times, want 0", n)
			}
		})
	}
}

func TestImageDropsFragment(t *testing.T) {
	svc, f := newImages(t, nil)
	if _, err := svc.Image(t.Context(), imgRepo, "attachments/a.png#x"); err != nil {
		t.Fatal(err)
	}
	calls := f.callList()
	if len(calls) != 1 || strings.Contains(calls[0].url, "#") {
		t.Errorf("OpenAsset calls = %+v, want one request without a fragment", calls)
	}
	if _, ok, err := svc.PeekImage(imgRepo, "attachments/a.png"); !ok || err != nil {
		t.Errorf("PeekImage without the fragment = (ok %v, err %v), want a hit on the same entry", ok, err)
	}
}

func TestImageWithoutAssetReaderIsUnsupported(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h"})
	svc := core.New(noAssets{Forge: f}, core.Options{})
	if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("Image error = %v, want ErrUnsupported", err)
	}
	if _, ok, err := svc.PeekImage(imgRepo, "a.png"); !ok || !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("PeekImage = (ok %v, err %v), want (true, ErrUnsupported)", ok, err)
	}
}

func TestImageDecodesSupportedFormats(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	tests := []struct {
		name string
		body []byte
		at   image.Point
		want color.RGBA
		tol  int
	}{
		{"png", solidPNG(t, 2, 2, red), image.Pt(1, 1), red, 0},
		{"jpeg", fixture(t, "solid16.jpg"), image.Pt(8, 8), color.RGBA{200, 40, 40, 255}, 6},
		{"gif takes the first frame", fixture(t, "two_frame.gif"), image.Pt(1, 1), red, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newImages(t, serveBytes(tt.body))
			img, err := svc.Image(t.Context(), imgRepo, "a.png")
			if err != nil {
				t.Fatal(err)
			}
			if got := rgbaAt(img, tt.at.X, tt.at.Y); !near(got, tt.want, tt.tol) {
				t.Errorf("pixel %v = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestImageDownscalesLongestSideTo1600(t *testing.T) {
	tests := []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"wide", 3200, 1000, 1600, 500},
		{"very wide", 4000, 3, 1600, 1},
		{"other side rounds to zero", 4000, 1, 1600, 1},
		{"tall", 1000, 2400, 667, 1600},
		{"at the limit is unchanged", 1600, 1600, 1600, 1600},
		{"small is unchanged", 300, 200, 300, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newImages(t, serveBytes(solidPNG(t, tt.w, tt.h, color.RGBA{10, 200, 30, 255})))
			img, err := svc.Image(t.Context(), imgRepo, "a.png")
			if err != nil {
				t.Fatal(err)
			}
			b := img.Bounds()
			if b.Dx() != tt.wantW || b.Dy() != tt.wantH {
				t.Errorf("size %dx%d, want %dx%d", b.Dx(), b.Dy(), tt.wantW, tt.wantH)
			}
			if b.Min != (image.Point{}) {
				t.Errorf("Bounds().Min = %v, want (0,0)", b.Min)
			}
		})
	}
}

func TestImageDownscaleKeepsColour(t *testing.T) {
	want := color.RGBA{10, 200, 30, 255}
	svc, _ := newImages(t, serveBytes(solidPNG(t, 3200, 1000, want)))
	img, err := svc.Image(t.Context(), imgRepo, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(img, 800, 250); !near(got, want, 1) {
		t.Errorf("centre pixel = %v, want %v", got, want)
	}
}

func TestImageDownscaleKeepsTransparency(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3200, 1000))
	for y := range 1000 {
		for x := 1600; x < 3200; x++ {
			src.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	svc, _ := newImages(t, serveBytes(b.Bytes()))
	img, err := svc.Image(t.Context(), imgRepo, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := rgbaAt(img, 100, 250); got.A != 0 {
		t.Errorf("transparent region alpha = %d, want 0", got.A)
	}
	if got := rgbaAt(img, 1500, 250); got.A != 255 || !near(got, color.RGBA{255, 255, 255, 255}, 1) {
		t.Errorf("opaque region = %v, want opaque white", got)
	}
}

func TestImageDownscalesFirstFrameWithOffset(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	svc, _ := newImages(t, serveBytes(fixture(t, "offset_frame.gif")))
	img, err := svc.Image(t.Context(), imgRepo, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 1600 || b.Dy() != 640 || b.Min != (image.Point{}) {
		t.Errorf("bounds %v, want (0,0)-(1600,640)", b)
	}
	if got := rgbaAt(img, 800, 320); !near(got, red, 1) {
		t.Errorf("centre pixel = %v, want %v", got, red)
	}
}

func TestImageByteLimit(t *testing.T) {
	t.Run("one byte over 10 MiB is too large", func(t *testing.T) {
		svc, _ := newImages(t, serveBytes(make([]byte, 10<<20+1)))
		if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, core.ErrImageTooLarge) {
			t.Errorf("got %v, want ErrImageTooLarge", err)
		}
	})
	t.Run("exactly 10 MiB of non-image data is a decode error", func(t *testing.T) {
		svc, _ := newImages(t, serveBytes(make([]byte, 10<<20)))
		_, err := svc.Image(t.Context(), imgRepo, "a.png")
		if err == nil || errors.Is(err, core.ErrImageTooLarge) {
			t.Errorf("got %v, want a non-size error", err)
		}
	})
}

func TestImagePixelLimit(t *testing.T) {
	t.Run("8000x5001 is too large before any pixel data is decoded", func(t *testing.T) {
		svc, _ := newImages(t, serveBytes(pngHeaderOnly(8000, 5001)))
		if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, core.ErrImageTooLarge) {
			t.Errorf("got %v, want ErrImageTooLarge", err)
		}
	})
	t.Run("8000x5000 is within the limit, so only the missing data fails", func(t *testing.T) {
		svc, _ := newImages(t, serveBytes(pngHeaderOnly(8000, 5000)))
		_, err := svc.Image(t.Context(), imgRepo, "a.png")
		if err == nil || errors.Is(err, core.ErrImageTooLarge) {
			t.Errorf("got %v, want a non-size error", err)
		}
	})
}

func TestImageUnsupportedFormat(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"plain text", "hello, this is not an image"},
		{"webp", "RIFF\x00\x00\x00\x00WEBPVP8 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newImages(t, serveBytes([]byte(tt.body)))
			if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, image.ErrFormat) {
				t.Errorf("got %v, want image.ErrFormat", err)
			}
		})
	}
}

func TestImageFetchDeadlineIsTwentySeconds(t *testing.T) {
	svc, f := newImages(t, nil)
	before := time.Now()
	if _, err := svc.Image(t.Context(), imgRepo, "a.png"); err != nil {
		t.Fatal(err)
	}
	dl, ok := f.callList()[0].ctx.Deadline()
	if !ok {
		t.Fatal("the context passed to OpenAsset has no deadline")
	}
	if got := dl.Sub(before); got > imageTimeout+250*time.Millisecond || got < imageTimeout-time.Second {
		t.Errorf("deadline is %v after the call, want about %v", got, imageTimeout)
	}
}

func TestImageHoldsSlotOnlyUntilBodyRead(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	svc, _ := newImagesOpts(t, blockingServe(started, release), core.Options{MaxConcurrent: 1})
	imgDone := make(chan error, 1)
	go func() {
		_, err := svc.Image(context.Background(), imgRepo, "a.png")
		imgDone <- err
	}()
	waitForOpen(t, started)

	reposDone := make(chan error, 1)
	go func() {
		_, err := svc.Repos(context.Background())
		reposDone <- err
	}()
	select {
	case <-reposDone:
		t.Fatal("Repos ran while the image held the only slot")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := recvErr(t, imgDone, "Image"); err != nil {
		t.Fatal(err)
	}
	if err := recvErr(t, reposDone, "Repos"); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedImageDoesNotWaitForSlot(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	svc, _ := newImagesOpts(t, blockingServe(started, release), core.Options{MaxConcurrent: 1})
	imgDone := make(chan error, 1)
	go func() {
		_, err := svc.Image(context.Background(), imgRepo, "a.png")
		imgDone <- err
	}()
	waitForOpen(t, started)

	refused := make(chan error, 1)
	go func() {
		_, err := svc.Image(context.Background(), imgRepo, "https://other/x.png")
		refused <- err
	}()
	if err := recvErr(t, refused, "refused Image"); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("refused Image = %v, want ErrUnsupported", err)
	}

	close(release)
	if err := recvErr(t, imgDone, "Image"); err != nil {
		t.Fatal(err)
	}
}

func TestPeekAfterFetch(t *testing.T) {
	svc, f := newImages(t, nil)
	if img, ok, err := svc.PeekImage(imgRepo, "attachments/a.png"); img != nil || ok || err != nil {
		t.Fatalf("PeekImage before any fetch = (%v, %v, %v), want (nil, false, nil)", img, ok, err)
	}
	fetched, err := svc.Image(t.Context(), imgRepo, "attachments/a.png")
	if err != nil {
		t.Fatal(err)
	}
	calls := len(f.callList())
	for _, raw := range []string{"attachments/a.png", "/o/r/attachments/a.png"} {
		img, ok, err := svc.PeekImage(imgRepo, raw)
		if !ok || err != nil || img == nil || img.Bounds() != fetched.Bounds() {
			t.Errorf("PeekImage(%q) = (%v, %v, %v), want the fetched image", raw, img, ok, err)
		}
	}
	if n := len(f.callList()); n != calls {
		t.Errorf("peeks reached the forge: %d OpenAsset calls, want %d", n, calls)
	}
}

func TestPeekReturnsCachedFailure(t *testing.T) {
	gone := fmt.Errorf("gone: %w", forge.ErrNotFound)
	svc, f := newImages(t, func(context.Context, *url.URL) (io.ReadCloser, error) {
		return nil, gone
	})
	if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("Image = %v, want ErrNotFound", err)
	}
	calls := len(f.callList())
	img, ok, err := svc.PeekImage(imgRepo, "a.png")
	if img != nil || !ok || !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("PeekImage = (%v, %v, %v), want (nil, true, ErrNotFound)", img, ok, err)
	}
	if n := len(f.callList()); n != calls {
		t.Errorf("peek reached the forge: %d OpenAsset calls, want %d", n, calls)
	}
}

func TestFailureCachingDependsOnCallerContext(t *testing.T) {
	t.Run("the service's own timeout is cached", func(t *testing.T) {
		svc, _ := newImages(t, func(context.Context, *url.URL) (io.ReadCloser, error) {
			return nil, context.DeadlineExceeded
		})
		if _, err := svc.Image(t.Context(), imgRepo, "a.png"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Image = %v, want DeadlineExceeded", err)
		}
		_, ok, err := svc.PeekImage(imgRepo, "a.png")
		if !ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("PeekImage = (ok %v, err %v), want a cached DeadlineExceeded", ok, err)
		}
	})

	t.Run("a caller cancelled before the fetch is not cached", func(t *testing.T) {
		svc, _ := newImages(t, nil)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := svc.Image(ctx, imgRepo, "a.png"); err == nil {
			t.Fatal("Image with a cancelled context returned no error")
		}
		if _, ok, _ := svc.PeekImage(imgRepo, "a.png"); ok {
			t.Error("the cancelled call was cached")
		}
	})

	t.Run("a caller cancelled during the fetch is not cached", func(t *testing.T) {
		started := make(chan struct{})
		svc, _ := newImages(t, func(ctx context.Context, _ *url.URL) (io.ReadCloser, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := svc.Image(ctx, imgRepo, "a.png")
			done <- err
		}()
		waitForOpen(t, started)
		cancel()
		if err := recvErr(t, done, "Image"); err == nil {
			t.Fatal("Image cancelled mid-fetch returned no error")
		}
		if _, ok, _ := svc.PeekImage(imgRepo, "a.png"); ok {
			t.Error("the cancelled fetch was cached")
		}
	})
}

func TestImageTimeoutDuringBodyRead(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 20 s service timeout")
	}
	svc, _ := newImages(t, func(ctx context.Context, _ *url.URL) (io.ReadCloser, error) {
		return io.NopCloser(ctxReader{ctx}), nil
	})
	if _, err := svc.Image(context.Background(), imgRepo, "a.png"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Image = %v, want the service's DeadlineExceeded", err)
	}
	_, ok, err := svc.PeekImage(imgRepo, "a.png")
	if !ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("PeekImage = (ok %v, err %v), want the timeout cached while the caller is live", ok, err)
	}
}

func TestImageLRUHoldsSixteenEntries(t *testing.T) {
	t.Run("the first URL is evicted by the 17th", func(t *testing.T) {
		svc, _ := newImages(t, nil)
		for i := range 17 {
			if _, err := svc.Image(t.Context(), imgRepo, imageNamed(i)); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok, _ := svc.PeekImage(imgRepo, imageNamed(0)); ok {
			t.Error("the first URL is still cached after 17 fetches")
		}
		if _, ok, _ := svc.PeekImage(imgRepo, imageNamed(1)); !ok {
			t.Error("the second URL was evicted")
		}
	})

	t.Run("a peek hit counts as use", func(t *testing.T) {
		svc, _ := newImages(t, nil)
		for i := range 16 {
			if _, err := svc.Image(t.Context(), imgRepo, imageNamed(i)); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok, _ := svc.PeekImage(imgRepo, imageNamed(0)); !ok {
			t.Fatal("the first URL is not cached after 16 fetches")
		}
		if _, err := svc.Image(t.Context(), imgRepo, imageNamed(16)); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := svc.PeekImage(imgRepo, imageNamed(1)); ok {
			t.Error("the second URL survived; the 17th fetch should have evicted it")
		}
		if _, ok, _ := svc.PeekImage(imgRepo, imageNamed(0)); !ok {
			t.Error("the peeked URL was evicted")
		}
	})
}

func TestClearImages(t *testing.T) {
	t.Run("cached URLs peek as unknown", func(t *testing.T) {
		svc, _ := newImages(t, nil)
		if _, err := svc.Image(t.Context(), imgRepo, "a.png"); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := svc.PeekImage(imgRepo, "a.png"); !ok {
			t.Fatal("the image is not cached before the clear")
		}
		svc.ClearImages()
		if _, ok, _ := svc.PeekImage(imgRepo, "a.png"); ok {
			t.Error("the image is still cached after ClearImages")
		}
	})

	t.Run("a fetch that started before the clear is not stored", func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		svc, _ := newImages(t, blockingServe(started, release))
		done := make(chan error, 1)
		go func() {
			_, err := svc.Image(context.Background(), imgRepo, "a.png")
			done <- err
		}()
		waitForOpen(t, started)
		svc.ClearImages()
		close(release)
		if err := recvErr(t, done, "Image"); err != nil {
			t.Fatalf("the in-flight Image returned %v, want its image", err)
		}
		if _, ok, _ := svc.PeekImage(imgRepo, "a.png"); ok {
			t.Error("the fetch that started before the clear was stored after it")
		}
	})
}

func TestImageCallsAreRaceFree(t *testing.T) {
	svc, _ := newImages(t, nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				raw := imageNamed(i % 5)
				if _, err := svc.Image(context.Background(), imgRepo, raw); err != nil {
					t.Errorf("Image(%s): %v", raw, err)
				}
				_, _, _ = svc.PeekImage(imgRepo, raw)
				if i%7 == 0 {
					svc.ClearImages()
				}
			}
		}()
	}
	wg.Wait()
}
