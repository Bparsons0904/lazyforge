package termimg

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestSeamDeleteAndSingleChunk(t *testing.T) {
	if got, want := Delete(16, false), "\x1b_Gq=2,i=16,d=I,a=d\x1b\\"; got != want {
		t.Errorf("Delete(16, false) = %q, want %q", got, want)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if got := Transmit(16, img, 2, 2, false); !strings.Contains(got, "a=T;iVBOR") {
		t.Errorf("Transmit header missing a=T;iVBOR: %q", got)
	}
}

// Every chunk but the last must carry m=1, the last m=0, and the joined payload must be a PNG.
func TestSeamTransmitChunks(t *testing.T) {
	img := seamNoise(120, 120) // random pixels keep the PNG large enough to span several chunks
	parts := strings.Split(Transmit(16, img, 4, 2, false), "\x1b\\")
	parts = parts[:len(parts)-1] // Split leaves an empty tail after the last terminator
	if len(parts) < 2 {
		t.Fatalf("image fit in one chunk (%d command); the check needs several", len(parts))
	}

	var payload strings.Builder
	for i, part := range parts {
		header, data, _ := strings.Cut(strings.TrimPrefix(part, "\x1b_G"), ";")
		want := ",m=1"
		if i == len(parts)-1 {
			want = ",m=0"
		}
		if !strings.HasSuffix(header, want) {
			t.Errorf("chunk %d header %q does not end with %q", i, header, want)
		}
		payload.WriteString(data)
	}

	raw, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("joined payload is not base64: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("joined payload is not a PNG: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Errorf("decoded bounds %v, want %v", decoded.Bounds(), img.Bounds())
	}
}

func seamNoise(w, h int) *image.RGBA {
	src := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(src.Uint32())
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255 // opaque, so the PNG round trip keeps the colour values
	}
	return img
}
