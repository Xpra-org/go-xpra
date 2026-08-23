package client

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
	"github.com/Xpra-org/go-xpra/ui"
)

type monitorHelloDisplay struct {
	ui.Display
	monitors []ui.Monitor
}

func (d *monitorHelloDisplay) Monitors() []ui.Monitor { return d.monitors }

// The expected digests were produced by xpra's own xpra.net.digest.gendigest,
// so this pins the exact HMAC operand order — which differs between the two
// stages and is not something a round-trip test would catch.
func TestChallengeDigest(t *testing.T) {
	serverSalt, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	clientSalt, _ := hex.DecodeString("6465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f80818283")

	const wantIntermediate = "50ebb9df76366d2abbf5513d2d2ab3bb2359affc7253d382afc4af399e33334f"
	const wantResponse = "4270a74c31fef12fd99af184d5e7314b9c0f11445e92887de7e013aff65da578"

	// Stage one keys on the client salt and messages the server salt.
	intermediate := hmacHex(clientSalt, serverSalt)
	if intermediate != wantIntermediate {
		t.Errorf("intermediate salt = %s, want %s", intermediate, wantIntermediate)
	}
	// Stage two keys on the password and messages the hex form of stage one.
	if got := hmacHex([]byte("sekrit"), []byte(intermediate)); got != wantResponse {
		t.Errorf("response = %s, want %s", got, wantResponse)
	}
}

func challengePacket(digest, saltDigest string) protocol.Packet {
	return protocol.Packet{"challenge", []byte("server-salt"),
		map[string]any{}, digest, saltDigest, "password"}
}

func TestChallengeReply(t *testing.T) {
	response, salt, err := challengeReply(challengePacket(digestName, digestName), "sekrit")
	if err != nil {
		t.Fatalf("challengeReply: %v", err)
	}
	if len(salt) != challengeSaltLen {
		t.Errorf("client salt is %d bytes, want %d", len(salt), challengeSaltLen)
	}
	if len(response) != 64 {
		t.Errorf("response is %d characters, want 64 hex characters", len(response))
	}
	// The response must be reproducible from the salt we hand back, since the
	// server repeats the same computation with it.
	if want := hmacHex([]byte("sekrit"), []byte(hmacHex(salt, []byte("server-salt")))); response != want {
		t.Errorf("response is not consistent with the returned client salt")
	}
	// A fresh salt each time, or the exchange would be replayable.
	_, salt2, _ := challengeReply(challengePacket(digestName, digestName), "sekrit")
	if string(salt) == string(salt2) {
		t.Error("the client salt is not random")
	}
}

// A digest we cannot compute must fail loudly rather than send a wrong answer.
func TestChallengeReplyRejectsOtherDigests(t *testing.T) {
	cases := []struct{ digest, saltDigest string }{
		{"xor", "xor"},
		{"hmac+sha512", digestName},
		{digestName, "xor"},
		{"des", "des"},
	}
	for _, tc := range cases {
		if _, _, err := challengeReply(challengePacket(tc.digest, tc.saltDigest), "pw"); err == nil {
			t.Errorf("challengeReply accepted digest %q/%q", tc.digest, tc.saltDigest)
		}
	}
	// A challenge with no server salt is malformed.
	if _, _, err := challengeReply(protocol.Packet{"challenge"}, "pw"); err == nil {
		t.Error("challengeReply accepted a challenge with no salt")
	}
}

// The digest name may carry a colon-separated suffix, which must be stripped
// before comparison (xpra/client/base/challenge.py:303).
func TestChallengeReplyAcceptsSuffixedDigest(t *testing.T) {
	if _, _, err := challengeReply(challengePacket(digestName+":something", digestName), "pw"); err != nil {
		t.Errorf("challengeReply rejected a suffixed digest: %v", err)
	}
}

// The hello has to encode cleanly and carry the handful of keys the server
// treats as mandatory.
func TestBuildHello(t *testing.T) {
	setBackwardsCompatible(t, true)
	caps := buildHello("", false)
	encoded, err := rencodeplus.Encode([]any{"hello", caps})
	if err != nil {
		t.Fatalf("the hello does not encode: %v", err)
	}
	decoded, err := rencodeplus.Decode(encoded)
	if err != nil {
		t.Fatalf("the hello does not decode: %v", err)
	}
	packet := protocol.Packet(decoded.([]any))
	got := packet.Dict(1)

	if got.Str("version") == "" {
		t.Error("version is required: the server disconnects without it")
	}
	if !got.Has("encoders") {
		t.Error("encoders is required: the server disconnects if encoder negotiation fails")
	}
	if !got.Bool("chunks") {
		t.Error("chunks must be enabled to receive large binary values out of band")
	}
	if !got.Bool("events") {
		t.Error("events must be enabled to receive server lifecycle events")
	}
	if !got.Bool("bell") {
		t.Error("bell must be enabled to receive forwarded bell events")
	}
	if !got.Bool("show-desktop") {
		t.Error("show-desktop must be enabled to receive minimize and restore requests")
	}
	cursor := got.Dict("cursor")
	if cursor == nil || !cursor.Bool("backwards-compatible") {
		t.Error("the backwards-compatible cursor capability must be enabled")
	}
	cursorEncodings, _ := cursor["encodings"].([]any)
	if len(cursorEncodings) != 1 || cursorEncodings[0] != "png" {
		t.Errorf("cursor encodings = %v, want [png]", cursorEncodings)
	}
	if !got.Bool("cursors") {
		t.Error("the legacy cursors capability must be enabled")
	}
	if window := got.Dict("window"); window == nil || !window.Bool("enabled") {
		t.Error("window.enabled must be advertised for window forwarding")
	}
	if !got.Bool("windows") {
		t.Error("the legacy windows capability must be enabled")
	}
	if !got.Bool("notification") {
		t.Error("the modern notification capability must be enabled")
	}
	notifications := got.Dict("notifications")
	if notifications == nil || !notifications.Bool("enabled") {
		t.Error("the compatibility notifications.enabled capability must be enabled")
	}
	encoding := got.Dict("encoding")
	if encoding == nil {
		t.Fatal("the encoding capabilities are missing")
	}
	// Auto lets the server choose among every codec advertised in core.
	if encoding.Str("setting") != "auto" {
		t.Errorf("encoding.setting = %q, want auto", encoding.Str("setting"))
	}
	for _, key := range []string{"core", "rgb_formats"} {
		if !encoding.Has(key) {
			t.Errorf("encoding.%s is missing", key)
		}
	}
	iconEncodings, _ := encoding["window-icon"].([]any)
	if len(iconEncodings) != 1 || iconEncodings[0] != "png" {
		t.Errorf("window icon encodings = %v, want [png]", iconEncodings)
	}
	core, _ := encoding["core"].([]any)
	for _, want := range []string{"rgb24", "rgb32", "jpeg", "png", "png/P", "png/L", "webp"} {
		found := false
		for _, got := range core {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("encoding.core does not contain %q: %v", want, core)
		}
	}
	// Advertising these would make the server compress pixel data, which this
	// client does not decompress.
	for _, key := range []string{"lz4", "zstd", "rgb_lz4", "rgb_zstd"} {
		if got.Has(key) || encoding.Has(key) {
			t.Errorf("%q must not be advertised: it enables pixel compression", key)
		}
	}
	// A truthy "challenge" key makes a no-auth server send a fake challenge.
	if got.Has("challenge") {
		t.Error("the hello must not carry a challenge key")
	}
	// "wants" would replace the server's default list rather than extend it.
	if got.Has("wants") {
		t.Error("the hello must not carry a wants key")
	}
}

func TestBuildHelloWithoutCompatibility(t *testing.T) {
	setBackwardsCompatible(t, false)
	encoded, err := rencodeplus.Encode([]any{"hello", buildHello("", false)})
	if err != nil {
		t.Fatalf("encoding hello: %v", err)
	}
	decoded, err := rencodeplus.Decode(encoded)
	if err != nil {
		t.Fatalf("decoding hello: %v", err)
	}
	got := protocol.Packet(decoded.([]any)).Dict(1)

	if got.Dict("pointer") == nil {
		t.Error("modern pointer capability is missing")
	}
	cursor := got.Dict("cursor")
	if cursor == nil {
		t.Fatal("modern cursor capability is missing")
	}
	if cursor.Bool("backwards-compatible") {
		t.Error("cursor backwards-compatible capability is enabled")
	}
	if window := got.Dict("window"); window == nil || !window.Bool("enabled") {
		t.Error("window.enabled must be advertised for window forwarding")
	}
	for _, key := range []string{"rencodeplus", "windows", "mouse", "cursors", "notifications"} {
		if got.Has(key) {
			t.Errorf("legacy %q capability is advertised", key)
		}
	}
	if !got.Bool("notification") {
		t.Error("modern notification capability is not enabled")
	}
}

func TestBuildHelloClipboardCapability(t *testing.T) {
	encoded, err := rencodeplus.Encode([]any{"hello", buildHello("", true)})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := rencodeplus.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	clipboard := protocol.Packet(decoded.([]any)).Dict(1).Dict("clipboard")
	if clipboard == nil || !clipboard.Bool("enabled") {
		t.Fatal("clipboard capability is missing or disabled")
	}
	for _, key := range []string{"selections", "greedy", "want_targets", "preferred-targets"} {
		if !clipboard.Has(key) {
			t.Errorf("clipboard.%s is missing", key)
		}
	}
}

func TestHelloIncludesPerMonitorCapabilities(t *testing.T) {
	setBackwardsCompatible(t, false)
	client, server, _ := outboundHarness(t)
	client.display = &monitorHelloDisplay{monitors: []ui.Monitor{
		{Geometry: ui.Rectangle{}}, // invalid outputs are not advertised
		{
			Name: "DP-1", Manufacturer: "Acme", Model: "Wide Panel",
			SubpixelLayout: "horizontal-rgb",
			Geometry:       ui.Rectangle{X: -1920, Y: 0, Width: 1920, Height: 1080},
			WorkArea:       ui.Rectangle{X: -1920, Y: 24, Width: 1920, Height: 1056},
			WidthMM:        510, HeightMM: 290, RefreshRate: 60000, ScaleFactor: 1,
			Primary: true,
		},
	}}
	if err := client.sendHello("", nil); err != nil {
		t.Fatalf("sendHello: %v", err)
	}
	if len(client.monitors) != 1 || client.monitors[0].Name != "DP-1" {
		t.Fatalf("advertised monitor snapshot = %#v", client.monitors)
	}
	hello := receiveOutbound(t, server, "hello").Dict(1)
	display := hello.Dict("display")
	if display == nil {
		t.Fatal("display subsystem capabilities are missing")
	}
	monitors := display.Dict("monitors")
	if monitors == nil || len(monitors) != 1 {
		t.Fatalf("monitors = %#v, want one valid monitor", monitors)
	}
	monitor := monitors.Dict("0")
	if monitor == nil {
		t.Fatal("monitor 0 is missing")
	}
	if got := monitor["geometry"]; !reflect.DeepEqual(got,
		[]any{int64(-1920), int64(0), int64(1920), int64(1080)}) {
		t.Errorf("geometry = %#v", got)
	}
	if got := monitor["workarea"]; !reflect.DeepEqual(got,
		[]any{int64(-1920), int64(24), int64(1920), int64(1056)}) {
		t.Errorf("workarea = %#v", got)
	}
	if monitor.Str("name") != "DP-1" || monitor.Str("manufacturer") != "Acme" ||
		monitor.Str("model") != "Wide Panel" ||
		monitor.Str("subpixel-layout") != "horizontal-rgb" {
		t.Errorf("monitor identity = %#v", monitor)
	}
	if monitor.Int("width-mm") != 510 || monitor.Int("height-mm") != 290 ||
		monitor.Int("refresh-rate") != 60000 || monitor.Int("scale-factor") != 1 ||
		!monitor.Bool("primary") {
		t.Errorf("monitor details = %#v", monitor)
	}
}

func TestBuildHelloUsesExplicitUsername(t *testing.T) {
	t.Setenv("USER", "environment-user")
	got := buildHello("url-user", false)
	if username := helloString(got, "username"); username != "url-user" {
		t.Errorf("username = %q, want url-user", username)
	}
	if user := helloString(got, "user"); user != "url-user" {
		t.Errorf("user = %q, want url-user", user)
	}
}

func TestBuildHelloFallsBackToEnvironmentUsername(t *testing.T) {
	t.Setenv("USER", "environment-user")
	got := buildHello("", false)
	if username := helloString(got, "username"); username != "environment-user" {
		t.Errorf("username = %q, want environment-user", username)
	}
}

func helloString(caps rencodeplus.Dict, key string) string {
	for _, entry := range caps {
		if entry.Key == key {
			value, _ := entry.Value.(string)
			return value
		}
	}
	return ""
}

func TestMachineUUIDIsStable(t *testing.T) {
	if first, second := machineUUID(), machineUUID(); first != second {
		t.Errorf("machineUUID is not stable: %q then %q", first, second)
	}
	if len(machineUUID()) != 32 {
		t.Errorf("machineUUID is %d characters, want 32 hex characters", len(machineUUID()))
	}
}

func TestPingsOnlyStartWhenTheServerAdvertisesThem(t *testing.T) {
	tests := []struct {
		name                string
		backwardsCompatible bool
		caps                map[string]any
		want                bool
	}{
		{"advertised interval", false, map[string]any{"ping": int64(5)}, true},
		{"advertised flag", false, map[string]any{"ping": true}, true},
		{"disabled", false, map[string]any{"ping": int64(0)}, false},
		// A --minimal server drops the key altogether and warns about every
		// ping it has no handler for, in either compatibility mode.
		{"absent", false, map[string]any{}, false},
		{"absent on a legacy server", true, map[string]any{}, false},
		{"advertised by a legacy server", true, map[string]any{"ping": int64(5)}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBackwardsCompatible(t, test.backwardsCompatible)
			client, _, _ := outboundHarness(t)
			client.configurePings(protocol.Dict(test.caps))
			defer client.stopPings()
			if got := client.pingTicks != nil; got != test.want {
				t.Errorf("pings started = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPingsStopWhenAServerReHelloDropsTheCapability(t *testing.T) {
	setBackwardsCompatible(t, false)
	client, _, _ := outboundHarness(t)
	client.configurePings(protocol.Dict{"ping": true})
	ticker := client.ping
	client.configurePings(protocol.Dict{"ping": true})
	if client.ping != ticker {
		t.Error("a second hello restarted the ping timer")
	}
	client.configurePings(protocol.Dict{})
	if client.ping != nil || client.pingTicks != nil {
		t.Error("pings kept running after the capability went away")
	}
}

// The advertised protocol floor is what a modern server runs through
// protocol_compat_check, and it must not soften in backwards compatible mode:
// the legacy spellings are additive, the packet shapes we understand are not.
func TestBuildHelloMinProtocolVersion(t *testing.T) {
	for _, compatible := range []bool{true, false} {
		setBackwardsCompatible(t, compatible)
		encoded, err := rencodeplus.Encode([]any{"hello", buildHello("", false)})
		if err != nil {
			t.Fatalf("encoding hello: %v", err)
		}
		decoded, err := rencodeplus.Decode(encoded)
		if err != nil {
			t.Fatalf("decoding hello: %v", err)
		}
		got := protocol.Packet(decoded.([]any)).Dict(1)["protocol-version"]
		if want := []any{int64(6), int64(5)}; !reflect.DeepEqual(got, want) {
			t.Errorf("backwards compatible %v: protocol-version = %#v, want %#v", compatible, got, want)
		}
	}
}
