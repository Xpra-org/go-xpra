package protocol

// MinProtocolVersion is the oldest peer we are prepared to talk to, advertised
// as the hello "protocol-version" capability and run through the remote's
// protocol_compat_check (xpra/util/version.py) to decide whether it can keep
// talking to us.
//
// Servers older than 7.0 are not supported. Advertising (7, 0) also makes a
// v7 or v8 server treat us as a modern peer (xpra/net/common.py legacy_peer),
// so it sends, and expects, the modern packet names only.
var MinProtocolVersion = [...]int{7, 0}
