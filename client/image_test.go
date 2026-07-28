package client

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDecodePNG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	src.SetNRGBA(1, 0, color.NRGBA{G: 0xff, A: 0xff})
	src.SetNRGBA(0, 1, color.NRGBA{B: 0xff, A: 0xff})
	src.SetNRGBA(1, 1, color.NRGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 0xff, 0xff, 0, 0xff, 0, 0xff,
		0xff, 0, 0, 0xff, 0xc0, 0x80, 0x40, 0xff,
	}
	for _, coding := range []string{"png", "png/P", "png/L"} {
		t.Run(coding, func(t *testing.T) {
			got, stride, err := decodeImage(coding, encoded.Bytes(), 2, 2)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if stride != 8 {
				t.Errorf("stride = %d, want 8", stride)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("pixels = %v, want %v", got, want)
			}
		})
	}
}

func TestDecodePNGVariants(t *testing.T) {
	paletted := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{
		color.RGBA{R: 0xff, A: 0xff},
		color.RGBA{G: 0xff, A: 0xff},
	})
	paletted.SetColorIndex(0, 0, 0)
	paletted.SetColorIndex(1, 0, 1)

	grayscale := image.NewGray(image.Rect(0, 0, 2, 1))
	grayscale.SetGray(0, 0, color.Gray{Y: 0x20})
	grayscale.SetGray(1, 0, color.Gray{Y: 0xe0})

	cases := []struct {
		coding string
		src    image.Image
		want   []byte
	}{
		{"png/P", paletted, []byte{0, 0, 0xff, 0xff, 0, 0xff, 0, 0xff}},
		{"png/L", grayscale, []byte{0x20, 0x20, 0x20, 0xff, 0xe0, 0xe0, 0xe0, 0xff}},
	}
	for _, tc := range cases {
		t.Run(tc.coding, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, tc.src); err != nil {
				t.Fatal(err)
			}
			got, stride, err := decodeImage(tc.coding, encoded.Bytes(), 2, 1)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if stride != 8 || !bytes.Equal(got, tc.want) {
				t.Errorf("got stride %d and pixels %v, want 8 and %v", stride, got, tc.want)
			}
		})
	}
}

func TestDecodeJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			src.SetRGBA(x, y, color.RGBA{R: 220, G: 40, B: 20, A: 0xff})
		}
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	got, stride, err := decodeImage("jpeg", encoded.Bytes(), 16, 8)
	if err != nil {
		t.Fatalf("decodeImage: %v", err)
	}
	if stride != 16*4 || len(got) != 16*8*4 {
		t.Fatalf("got stride %d and %d bytes, want %d and %d", stride, len(got), 16*4, 16*8*4)
	}
	// JPEG is lossy, so verify channel order and approximate colour rather
	// than tying the test to a decoder's exact rounding.
	b, g, r, x := got[0], got[1], got[2], got[3]
	if r < 200 || g > 60 || b > 40 || x != 0xff {
		t.Errorf("first BGRX pixel = %v, want approximately [20 40 220 255]", got[:4])
	}
}

func TestDecodeWebP(t *testing.T) {
	// A 2x2 lossy WebP containing solid red pixels. Keeping the tiny encoded
	// fixture inline makes this test independent of external WebP tools.
	encoded, err := hex.DecodeString(
		"524946463c000000574542505650382030000000d001009d012a02000200" +
			"02003425a00274ba01f80003b000fef0c40bff20b96175c8d7ff203fe407" +
			"fc80fff8f2000000")
	if err != nil {
		t.Fatal(err)
	}
	got, stride, err := decodeImage("webp", encoded, 2, 2)
	if err != nil {
		t.Fatalf("decodeImage: %v", err)
	}
	if stride != 2*4 || len(got) != 2*2*4 {
		t.Fatalf("got stride %d and %d bytes, want 8 and 16", stride, len(got))
	}
	// WebP is lossy, so check the channel order and approximate colour.
	b, g, r, x := got[0], got[1], got[2], got[3]
	if r < 200 || g > 60 || b > 40 || x != 0xff {
		t.Errorf("first BGRX pixel = %v, want approximately [0 0 255 255]", got[:4])
	}
}

func TestDecodeWebPVideoRangeBlack(t *testing.T) {
	// A 2x2 lossy WebP containing solid black. VP8 stores video-range YCbCr,
	// where black has luma 16 rather than zero.
	encoded, err := hex.DecodeString(
		"52494646240000005745425056503820180000003001009d012a02000200" +
			"02403825a400037000fefb9cc000")
	if err != nil {
		t.Fatal(err)
	}
	got, stride, err := decodeImage("webp", encoded, 2, 2)
	if err != nil {
		t.Fatalf("decodeImage: %v", err)
	}
	want := []byte{
		0, 0, 0, 0xff, 0, 0, 0, 0xff,
		0, 0, 0, 0xff, 0, 0, 0, 0xff,
	}
	if stride != 2*4 || !bytes.Equal(got, want) {
		t.Errorf("got stride %d and pixels %v, want 8 and %v", stride, got, want)
	}
}

func TestDecodeImageRejectsBadInput(t *testing.T) {
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name          string
		coding        string
		data          []byte
		width, height int
	}{
		{"truncated header", "png", valid.Bytes()[:12], 2, 2},
		{"truncated pixels", "png", valid.Bytes()[:len(valid.Bytes())-10], 2, 2},
		{"wrong codec", "jpeg", valid.Bytes(), 2, 2},
		{"wrong width", "png", valid.Bytes(), 3, 2},
		{"wrong height", "png", valid.Bytes(), 2, 3},
		{"unsupported", "avif", valid.Bytes(), 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := decodeImage(tc.coding, tc.data, tc.width, tc.height); err == nil {
				t.Error("decodeImage succeeded, want an error")
			}
		})
	}
}
