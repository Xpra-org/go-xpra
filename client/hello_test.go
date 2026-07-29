package client

import (
	"encoding/hex"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
)

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
	caps := buildHello("")
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
	encoded, err := rencodeplus.Encode([]any{"hello", buildHello("")})
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
	for _, key := range []string{"rencodeplus", "mouse", "cursors", "notifications"} {
		if got.Has(key) {
			t.Errorf("legacy %q capability is advertised", key)
		}
	}
	if !got.Bool("notification") {
		t.Error("modern notification capability is not enabled")
	}
}

func TestBuildHelloUsesExplicitUsername(t *testing.T) {
	t.Setenv("USER", "environment-user")
	got := buildHello("url-user")
	if username := helloString(got, "username"); username != "url-user" {
		t.Errorf("username = %q, want url-user", username)
	}
	if user := helloString(got, "user"); user != "url-user" {
		t.Errorf("user = %q, want url-user", user)
	}
}

func TestBuildHelloFallsBackToEnvironmentUsername(t *testing.T) {
	t.Setenv("USER", "environment-user")
	got := buildHello("")
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
