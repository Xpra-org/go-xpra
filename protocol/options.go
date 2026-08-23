package protocol

import (
	"os"
	"strconv"
	"strings"
)

// BackwardsCompatible controls whether legacy xpra packet types are accepted.
//
// It mirrors xpra.net.common.BACKWARDS_COMPATIBLE: the value is read once at
// process startup from XPRA_BACKWARDS_COMPATIBLE and defaults to true.
var BackwardsCompatible = envBool("XPRA_BACKWARDS_COMPATIBLE", true)

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch value {
	case "yes", "true", "on":
		return true
	case "no", "false", "off":
		return false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n != 0
}

// MinProtocolVersion is the oldest peer we are prepared to talk to, advertised
// as the hello "protocol" capability and run through the remote's
// protocol_compat_check (xpra/util/version.py:120) to decide whether it can
// keep talking to us.
//
// It mirrors xpra.net.common.MIN_PROTOCOL_VERSION, except that xpra lowers its
// own value to (5, 1) in backwards compatible mode. We do not: this client only
// implements the modern packet shapes, and BackwardsCompatible merely adds
// legacy spellings on top, so the floor stays where it is either way.
var MinProtocolVersion = [...]int{6, 5}
