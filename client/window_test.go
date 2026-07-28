package client

import (
	"math"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
)

func TestPacketWindowSize(t *testing.T) {
	tests := []struct {
		name                  string
		width, height         int64
		wantWidth, wantHeight int
	}{
		{"ordinary", 800, 600, 800, 600},
		{"minimum", 0, -1, 1, 1},
		{"ushort maximum", math.MaxUint16, math.MaxUint16, math.MaxUint16, math.MaxUint16},
		{"oversized", math.MaxInt64, math.MaxUint16 + 1, math.MaxUint16, math.MaxUint16},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := protocol.Packet{"window-create", int64(1), int64(0), int64(0),
				test.width, test.height}
			width, height := packetWindowSize(packet)
			if width != test.wantWidth || height != test.wantHeight {
				t.Errorf("packetWindowSize = %dx%d, want %dx%d",
					width, height, test.wantWidth, test.wantHeight)
			}
		})
	}
}
