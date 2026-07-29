// Package goxpra provides assets shared by the command and platform packages.
package goxpra

import _ "embed"

// XpraPNG is the project's full-resolution application icon.
//
//go:embed xpra.png
var XpraPNG []byte
