package x11

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
		want   []byte // always B,G,R,X as the X server expects
	}{
		// Already in the server's layout: a straight copy, including the
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
			conv, srcBytesPerPixel, err := converterFor(tc.format)
			if err != nil {
				t.Fatalf("converterFor(%q): %v", tc.format, err)
			}
			if srcBytesPerPixel != len(tc.src) {
				t.Errorf("bytes per pixel = %d, want %d", srcBytesPerPixel, len(tc.src))
			}
			got := make([]byte, 4)
			conv(got, tc.src)
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
		conv, bytesPerPixel, err := converterFor(format)
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
		conv(got, src)
		// In the X server's layout red is the third byte.
		if got[2] != 0xff || got[0] != 0 || got[1] != 0 {
			t.Errorf("%s: red became %v, want blue=0 green=0 red=255", format, got[:3])
		}
	}
}

func TestConvertersHandleMultiplePixels(t *testing.T) {
	conv, _, err := converterFor("RGB")
	if err != nil {
		t.Fatal(err)
	}
	src := []byte{1, 2, 3, 4, 5, 6} // two pixels
	got := make([]byte, 8)
	conv(got, src)
	want := []byte{3, 2, 1, 0xff, 6, 5, 4, 0xff}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestConverterForRejectsUnknownFormat(t *testing.T) {
	for _, format := range []string{"", "r210", "BGR565", "YUV420P"} {
		if _, _, err := converterFor(format); err == nil {
			t.Errorf("converterFor(%q) succeeded, want an error", format)
		}
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
		w, h := clampSize(tc.w, tc.h)
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("clampSize(%d, %d) = %d, %d; want %d, %d",
				tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}
