package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// clientVersion is what we report in the hello.
//
// The server runs version_compat_check (xpra/util/version.py:108) over this and
// rejects anything below 3.0 or unparseable. Claiming the version we were
// written against takes the "identical major.minor" fast path on a matching
// server and the "newer remote version should work" path on anything else.
const clientVersion = "6.6"

// digestName is the only challenge digest we implement. Advertising just this
// one in both digest lists forces the server's choose_digest
// (xpra/net/digest.py:70) to pick it, so there is exactly one hash to support.
const digestName = "hmac+sha256"

// challengeSaltLen matches what xpra's own client uses for non-xor digests
// (xpra/client/base/challenge.py:319).
const challengeSaltLen = 32

// buildHello assembles the capability dictionary.
//
// The shape here is load-bearing in ways that are not obvious, so the
// non-obvious keys carry the server-side reference that explains them. Anything
// not advertised is simply never sent to us, which is how this client avoids
// having to handle cursors, icons, notifications, clipboard and audio at all.
func buildHello() rencodeplus.Dict {
	caps := rencodeplus.Dict{
		{Key: "version", Value: clientVersion},
		{Key: "client_type", Value: "go"},

		// Packet encoder negotiation. Without a match the server disconnects
		// with PROTOCOL_ERROR (xpra/server/core.py:1567). "encoders" is the
		// modern key; the bare boolean is what a backwards-compatible server
		// falls back to (xpra/net/protocol/socket_handler.py:630).
		{Key: "encoders", Value: []string{"rencodeplus"}},
		{Key: "rencodeplus", Value: true},

		// Ask the server to inline binary payloads instead of splitting them
		// into out-of-band chunks (socket_handler.py:629). This removes the
		// entire chunk-reassembly path from the client.
		{Key: "chunks", Value: false},

		// Packet-level compression, inbound only: our own packets are small
		// input events. Note this is a different knob from the top-level "lz4"
		// key, which would additionally compress raw pixel data
		// (xpra/server/source/encoding.py:346) — we deliberately do not send it.
		{Key: "compressors", Value: []string{"lz4"}},
		{Key: "compression_level", Value: 1},

		{Key: "windows", Value: true},
		{Key: "keyboard", Value: true},
		{Key: "mouse", Value: true},
		{Key: "ping", Value: true},

		// Encodings. "core" is the hard filter the server intersects with its
		// own encoders (xpra/server/window/compress.py:937), so it has to name
		// real encoders — "rgb" is an alias accepted only for "setting".
		{Key: "encodings", Value: []string{"rgb24", "rgb32"}},
		{Key: "encoding", Value: rencodeplus.Dict{
			{Key: "options", Value: []string{"rgb24", "rgb32"}},
			{Key: "core", Value: []string{"rgb24", "rgb32"}},

			// "setting" pins the encoding, and unlike "core" it takes the
			// user-facing name: set_encoding validates it against the server's
			// advertised encodings (xpra/server/source/encoding.py:470), where
			// the raw formats appear collectively as "rgb". Passing "rgb32"
			// here makes the server log an error and fall back to "auto".
			{Key: "setting", Value: "rgb"},

			// The pixel layouts we can paint. This defaults to just ("RGB",)
			// server-side (xpra/server/source/encoding.py:69); asking for BGRX
			// and BGRA gets us the X server's native layout, which is also
			// byte-for-byte what our X11 framebuffer holds.
			{Key: "rgb_formats", Value: []string{"BGRX", "BGRA"}},
			{Key: "transparency", Value: false},

			// An empty list means "no window icon encodings", so the server
			// sends no window-icon packets.
			{Key: "window-icon", Value: []string{}},
		}},

		// Restrict window metadata to what we actually apply. Omitting this
		// gets a much longer default list (xpra/constants.py:132).
		// "override-redirect" is included because a server with backwards
		// compatibility disabled marks OR windows there rather than by using a
		// distinct packet type (xpra/server/subsystem/window.py:602).
		{Key: "metadata", Value: rencodeplus.Dict{
			{Key: "supported", Value: []string{
				"title", "window-type", "size-constraints", "override-redirect",
			}},
		}},

		// Advertising only one digest forces the server to choose it.
		{Key: "digest", Value: []string{digestName}},
		{Key: "salt-digest", Value: []string{digestName}},
	}

	// uuid and session-id are optional, but if present they must pass
	// valid_uuid or the server disconnects (xpra/os_util.py:194, used from
	// xpra/server/subsystem/client_session.py:152). Hex digests always do.
	caps.Set("uuid", machineUUID())
	caps.Set("session-id", randomHex(16))
	if host, err := os.Hostname(); err == nil {
		caps.Set("hostname", host)
	}
	if user := os.Getenv("USER"); user != "" {
		caps.Set("username", user)
		caps.Set("user", user)
	}
	return caps
}

// challengeReply computes the response to a server challenge packet.
//
// The exchange is two nested HMACs, and the operand order differs between them
// (xpra/client/base/challenge.py:305, xpra/net/digest.py:108):
//
//	salt     = hex(HMAC-SHA256(key: clientSalt, message: serverSalt))
//	response = hex(HMAC-SHA256(key: password,   message: salt))
//
// The intermediate salt is used as its 64-character ASCII hex form, not as raw
// digest bytes. The returned clientSalt must be sent back verbatim, since the
// server repeats the first step with it.
func challengeReply(packet protocol.Packet, password string) (response string, clientSalt []byte, err error) {
	serverSalt, ok := packet.Bytes(1)
	if !ok || len(serverSalt) == 0 {
		return "", nil, fmt.Errorf("challenge packet has no server salt")
	}
	// The digest name may carry a colon-separated suffix.
	digest, _, _ := strings.Cut(packet.Str(3), ":")
	saltDigest := "xor" // the server's default when the field is absent
	if len(packet) >= 5 {
		saltDigest = packet.Str(4)
	}
	if digest != digestName || saltDigest != digestName {
		return "", nil, fmt.Errorf("server asked for the %q/%q digests, but this client only implements %q",
			digest, saltDigest, digestName)
	}

	clientSalt = make([]byte, challengeSaltLen)
	if _, err := rand.Read(clientSalt); err != nil {
		return "", nil, fmt.Errorf("generating a client salt: %w", err)
	}
	salt := hmacHex(clientSalt, serverSalt)
	return hmacHex([]byte(password), []byte(salt)), clientSalt, nil
}

// hmacHex returns the lowercase hex HMAC-SHA256 of message under key, which is
// the form xpra puts on the wire.
func hmacHex(key, message []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

// password returns the password to answer a challenge with.
func password() string {
	return os.Getenv("XPRA_PASSWORD")
}

// machineUUID derives a stable per-machine identifier, falling back to a random
// one. The value is only used by the server to recognise a returning client.
func machineUUID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				// Hash rather than send the machine-id verbatim: it is meant to
				// be treated as confidential, and the server only needs
				// something stable.
				sum := sha256.Sum256([]byte("go-xpra:" + id))
				return hex.EncodeToString(sum[:16])
			}
		}
	}
	return randomHex(16)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice; a non-unique id is survivable.
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
