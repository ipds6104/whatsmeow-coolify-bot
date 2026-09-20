package whatsmeow

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestEnsureJPEG_AlreadyJPEG(t *testing.T) {
	// Magic bytes for JPEG: 0xFF, 0xD8, 0xFF
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	out, err := ensureJPEG(fakeJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, fakeJPEG) {
		t.Fatalf("expected untouched byte slice for already JPEG data")
	}
}

func TestEnsureJPEG_ConvertsPNGToJPEG(t *testing.T) {
	// Create a 2x2 test PNG in memory
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(1, 1, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("failed to encode test PNG: %v", err)
	}

	pngData := pngBuf.Bytes()
	// Confirm it's PNG
	if len(pngData) < 8 || string(pngData[1:4]) != "PNG" {
		t.Fatalf("test data is not PNG")
	}

	out, err := ensureJPEG(pngData)
	if err != nil {
		t.Fatalf("ensureJPEG failed on PNG input: %v", err)
	}

	// Confirm output starts with JPEG SOI magic bytes (0xFF, 0xD8, 0xFF)
	if len(out) < 3 || out[0] != 0xFF || out[1] != 0xD8 || out[2] != 0xFF {
		t.Fatalf("output is not valid JPEG magic bytes: %X", out[:3])
	}
}

func TestEnsureJPEG_InvalidDataPassesThrough(t *testing.T) {
	invalidData := []byte("not an image at all")
	out, err := ensureJPEG(invalidData)
	if err != nil {
		t.Fatalf("unexpected error on invalid data fallback: %v", err)
	}
	if !bytes.Equal(out, invalidData) {
		t.Fatalf("expected pass-through on non-image data")
	}
}
