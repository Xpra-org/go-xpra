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
// as the hello "protocol-version" capability and run through the remote's
// protocol_compat_check (xpra/util/version.py) to decide whether it can keep
// talking to us.
//
// xpra v8 uses (7, 0) in backwards compatible mode and (8, 0) otherwise. We
// stay at (7, 0) either way: this client supports v7 servers, and
// BackwardsCompatible merely adds legacy spellings on top of the modern packet
// shapes, so the floor does not depend on it.
var MinProtocolVersion = [...]int{7, 0}
