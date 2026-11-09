// Package version defines the go-xpra application version.
package version

// Version is reported by the command and in the client hello.
const Version = "0.4.0"

// Revision is the number of commits since the last tag. build.sh sets it with
// -ldflags "-X"; a plain `go build` leaves it empty.
var Revision string

// Full returns Version with the revision appended the way xpra does, such as
// "0.4.0-r3", or Version alone when the revision is unknown.
func Full() string {
	if Revision == "" {
		return Version
	}
	return Version + "-r" + Revision
}
