package ui

import (
	"bytes"
	"testing"
)

// The pixel converters decide whether the image comes out with red and blue
// swapped, which is the classic way to get this wrong.
func TestConverters(t *testing.T) {
	cases := []struct {
		format string
		src    []byte
		want   []byte // always B,G,R,X
	}{
		// Already in the destination layout: a straight copy, including the
		// unused fourth byte.
		{"BGRX", []byte{1, 2, 3, 0}, []byte{1, 2, 3, 0}},
		{"BGRA", []byte{1, 2, 3, 0xff}, []byte{1, 2, 3, 0xff}},
		// Red and blue swapped, alpha preserved in place.
		{"RGBX", []byte{3, 2, 1, 0}, []byte{1, 2, 3, 0}},
		{"RGBA", []byte{3, 2, 1, 0xff}, []byte{1, 2, 3, 0xff}},
		// Three-byte formats expand to four.
		{"BGR", []byte{1, 2, 3}, []byte{1, 2, 3, 0xff}},
		{"RGB", []byte{3, 2, 1}, []byte{1, 2, 3, 0xff}},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			convert, srcBytesPerPixel, err := ConverterFor(tc.format)
			if err != nil {
				t.Fatalf("ConverterFor(%q): %v", tc.format, err)
			}
			if srcBytesPerPixel != len(tc.src) {
				t.Errorf("bytes per pixel = %d, want %d", srcBytesPerPixel, len(tc.src))
			}
			got := make([]byte, 4)
			convert(got, tc.src)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("%s: got %v, want %v", tc.format, got, tc.want)
			}
		})
	}
}

// A pure red pixel is the clearest check that the channel order survives: if
// red and blue were transposed it would arrive as blue.
func TestConvertersPreserveRed(t *testing.T) {
	for _, format := range []string{"BGRX", "RGBX", "BGR", "RGB"} {
		convert, bytesPerPixel, err := ConverterFor(format)
		if err != nil {
			t.Fatal(err)
		}
		// Build one pure-red pixel in the source format.
		src := make([]byte, bytesPerPixel)
		if format[0] == 'R' {
			src[0] = 0xff // R first
		} else {
			src[2] = 0xff // B, G, then R
		}
		got := make([]byte, 4)
		convert(got, src)
		// In the destination layout red is the third byte.
		if got[2] != 0xff || got[0] != 0 || got[1] != 0 {
			t.Errorf("%s: red became %v, want blue=0 green=0 red=255", format, got[:3])
		}
	}
}

func TestConvertersHandleMultiplePixels(t *testing.T) {
	convert, _, err := ConverterFor("RGB")
	if err != nil {
		t.Fatal(err)
	}
	src := []byte{1, 2, 3, 4, 5, 6} // two pixels
	got := make([]byte, 8)
	convert(got, src)
	want := []byte{3, 2, 1, 0xff, 6, 5, 4, 0xff}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestConverterForRejectsUnknownFormat(t *testing.T) {
	for _, format := range []string{"", "r210", "BGR565", "YUV420P"} {
		if _, _, err := ConverterFor(format); err == nil {
			t.Errorf("ConverterFor(%q) succeeded, want an error", format)
		}
	}
}

// Convert has to honour both strides independently: the source is padded by the
// server, and the destination is a window wider than the damage rectangle.
func TestConvert(t *testing.T) {
	// Two rows of two BGRX pixels, with four bytes of source padding per row.
	src := []byte{
		1, 2, 3, 0, 4, 5, 6, 0, 0xaa, 0xaa, 0xaa, 0xaa,
		7, 8, 9, 0, 10, 11, 12, 0, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	// A destination three pixels wide, so each converted row leaves a gap.
	dst := make([]byte, 3*4*2)
	if err := Convert(dst, 3*4, src, 12, 2, 2, "BGRX"); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	want := []byte{
		1, 2, 3, 0, 4, 5, 6, 0, 0, 0, 0, 0,
		7, 8, 9, 0, 10, 11, 12, 0, 0, 0, 0, 0,
	}
	if !bytes.Equal(dst, want) {
		t.Errorf("got %v, want %v", dst, want)
	}
}

// Truncated pixel data must be reported rather than read past.
func TestConvertRejectsShortInput(t *testing.T) {
	cases := []struct {
		name                     string
		dst                      []byte
		dstStride                int
		src                      []byte
		srcStride, width, height int
		format                   string
	}{
		{"short source", make([]byte, 32), 8, make([]byte, 8), 8, 2, 2, "BGRX"},
		{"rowstride below the width", make([]byte, 32), 8, make([]byte, 32), 4, 2, 2, "BGRX"},
		{"short destination", make([]byte, 8), 8, make([]byte, 16), 8, 2, 2, "BGRX"},
		{"unknown format", make([]byte, 32), 8, make([]byte, 32), 8, 2, 2, "YUV420P"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Convert(tc.dst, tc.dstStride, tc.src, tc.srcStride, tc.width, tc.height, tc.format)
			if err == nil {
				t.Error("Convert succeeded, want an error")
			}
		})
	}
}

func TestClipDamage(t *testing.T) {
	cases := []struct {
		name                string
		x, y, width, height int
		wantW, wantH        int
	}{
		{"inside", 10, 10, 20, 20, 20, 20},
		{"overhanging", 90, 90, 20, 20, 10, 10},
		{"flush", 0, 0, 100, 100, 100, 100},
		{"outside", 100, 0, 10, 10, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := ClipDamage(tc.x, tc.y, tc.width, tc.height, 100, 100)
			if err != nil {
				t.Fatalf("ClipDamage: %v", err)
			}
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("got %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
	if _, _, err := ClipDamage(-1, 0, 10, 10, 100, 100); err == nil {
		t.Error("a negative origin was accepted, want an error")
	}
}

func TestClampSize(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{100, 50, 100, 50},
		{0, 0, 1, 1},              // CreateWindow rejects a zero dimension
		{-5, -5, 1, 1},            // as does a negative one
		{0x20000, 10, 0xffff, 10}, // the wire format caps dimensions at 16 bits
	}
	for _, tc := range cases {
		w, h := ClampSize(tc.w, tc.h)
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("ClampSize(%d, %d) = %d, %d; want %d, %d",
				tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}
