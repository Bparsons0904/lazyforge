package core

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	// Blank imports register the decoders image.Decode uses; a GIF shows its first frame.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const (
	imageTimeout   = 20 * time.Second // bounds the whole call, including the wait for a slot
	maxImageBytes  = 10 << 20
	maxImagePixels = 40_000_000
	maxImageSide   = 1600
	imageCacheSize = 16
)

// ErrImageTooLarge is returned, wrapped, when an image is over the byte or pixel limit.
var ErrImageTooLarge = errors.New("image too large")

// imageEntry is one cache slot: a decoded image or the failure that replaced it.
type imageEntry struct {
	key string
	img image.Image
	err error
}

// imageCache is a least-recently-used cache of imageEntry; the caller holds Service.mu.
type imageCache struct {
	gen   uint64 // bumped by reset so a fetch that started earlier does not store
	order *list.List
	byKey map[string]*list.Element
}

func newImageCache() *imageCache {
	return &imageCache{order: list.New(), byKey: map[string]*list.Element{}}
}

// get returns the entry for key and marks it most recent.
func (c *imageCache) get(key string) (imageEntry, bool) {
	el, ok := c.byKey[key]
	if !ok {
		return imageEntry{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(imageEntry), true
}

func (c *imageCache) put(e imageEntry) {
	if el, ok := c.byKey[e.key]; ok {
		el.Value = e
		c.order.MoveToFront(el)
		return
	}
	c.byKey[e.key] = c.order.PushFront(e)
	if c.order.Len() > imageCacheSize {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.byKey, oldest.Value.(imageEntry).key)
	}
}

func (c *imageCache) reset() {
	c.gen++
	c.order.Init()
	clear(c.byKey)
}

// Image fetches the image at raw, resolved against repo's page on the session's host, and returns it decoded and downscaled.
// A refused URL returns an error without any I/O.
func (s *Service) Image(ctx context.Context, repo domain.RepoRef, raw string) (image.Image, error) {
	u, ar, err := s.resolveImage(repo, raw)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	gen := s.images.gen
	s.mu.Unlock()
	ctx20, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	img, err := s.fetchImage(ctx20, ar, u)
	// A caller that navigated away must not pin its cancellation as a failure until the next r.
	if ctx.Err() == nil {
		s.storeImage(imageEntry{key: u.String(), img: img, err: err}, gen)
	}
	return img, err
}

// PeekImage returns the cached image or failure for raw without I/O.
// ok=false means nothing is cached; ok=true with err set means a refusal or a cached failure.
func (s *Service) PeekImage(repo domain.RepoRef, raw string) (image.Image, bool, error) {
	u, _, err := s.resolveImage(repo, raw)
	if err != nil {
		return nil, true, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.images.get(u.String())
	return e.img, ok, e.err
}

// ClearImages drops every cached image and failure; a fetch still in flight returns to its caller but is not stored.
func (s *Service) ClearImages() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images.reset()
}

// resolveImage returns the absolute URL to fetch for raw, refusing any URL that is off the session's host
// or unusable before any I/O.
func (s *Service) resolveImage(repo domain.RepoRef, raw string) (*url.URL, forge.AssetReader, error) {
	ar, ok := s.f.(forge.AssetReader)
	if !ok {
		return nil, nil, fmt.Errorf("%s doesn't support images: %w", s.f.Info().Kind, forge.ErrUnsupported)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, errors.New("image URL is empty")
	}
	// The parse error echoes raw, which may carry a query string, so it is not wrapped.
	ref, err := url.Parse(raw)
	if err != nil {
		return nil, nil, errors.New("image URL is malformed")
	}
	if ref.User != nil {
		return nil, nil, fmt.Errorf("image URL carries credentials: %w", forge.ErrUnsupported)
	}
	page, err := url.Parse(strings.TrimRight(s.f.Info().URL, "/") + "/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Name) + "/")
	if err != nil {
		return nil, nil, errors.New("forge URL is malformed")
	}
	abs := page.ResolveReference(ref)
	abs.Fragment, abs.RawFragment = "", ""
	if !forge.SameOrigin(abs, page) {
		return nil, nil, fmt.Errorf("image is not on the session's host: %w", forge.ErrUnsupported)
	}
	return abs, ar, nil
}

// fetchImage reads the body under a slot, then decodes it outside the slot.
func (s *Service) fetchImage(ctx context.Context, ar forge.AssetReader, u *url.URL) (image.Image, error) {
	body, err := s.readImage(ctx, ar, u)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	img, err := decodeImage(body)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// readImage holds a semaphore slot only while it opens and reads the body, so decoding does not starve other forge calls.
func (s *Service) readImage(ctx context.Context, ar forge.AssetReader, u *url.URL) ([]byte, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer func() { <-s.sem }()
	rc, err := ar.OpenAsset(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer func() { _ = rc.Close() }()
	// One byte past the limit tells "exactly at the limit" from "over it" without truncating.
	body, err := io.ReadAll(io.LimitReader(rc, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(body) > maxImageBytes {
		return nil, fmt.Errorf("image is over 10 MiB: %w", ErrImageTooLarge)
	}
	return body, nil
}

// decodeImage reads the header first, so an oversized image is refused before its pixels are allocated.
func decodeImage(body []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("image is over 40 MP: %w", ErrImageTooLarge)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return fitImage(img), nil
}

// fitImage returns img unchanged when its longest side is within maxImageSide.
func fitImage(img image.Image) image.Image {
	b := img.Bounds()
	if max(b.Dx(), b.Dy()) <= maxImageSide {
		return img
	}
	w, h := fitSize(b.Dx(), b.Dy())
	return boxDownscale(img, w, h)
}

// fitSize returns the size with the longest side at maxImageSide; the other side rounds half up and is at least 1.
func fitSize(w, h int) (int, int) {
	longest := max(w, h)
	if longest <= maxImageSide {
		return w, h
	}
	scale := func(n int) int {
		return max(1, int((int64(n)*maxImageSide*2+int64(longest))/int64(2*longest)))
	}
	if w >= h {
		return maxImageSide, scale(h)
	}
	return scale(w), maxImageSide
}

// boxDownscale averages the source box behind each destination pixel, in premultiplied RGBA.
func boxDownscale(src image.Image, w, h int) *image.RGBA {
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for dy := range h {
		y0, y1 := span(dy, h, sb.Dy())
		for dx := range w {
			x0, x1 := span(dx, w, sb.Dx())
			var sr, sg, sbl, sa, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, a := src.At(sb.Min.X+x, sb.Min.Y+y).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sbl += uint64(b)
					sa += uint64(a)
					n++
				}
			}
			dst.SetRGBA(dx, dy, color.RGBA{uint8(sr / n >> 8), uint8(sg / n >> 8), uint8(sbl / n >> 8), uint8(sa / n >> 8)})
		}
	}
	return dst
}

// span returns the half-open range of source pixels that destination cell i of n covers.
func span(i, n, size int) (lo, hi int) {
	return i * size / n, (i + 1) * size / n
}

// storeImage caches e unless the cache was reset after gen was read.
func (s *Service) storeImage(e imageEntry, gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.images.gen == gen {
		s.images.put(e)
	}
}
