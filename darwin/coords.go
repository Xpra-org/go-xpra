//go:build darwin

package darwin

// AppKit places windows and screens in a coordinate space with its origin at
// the bottom-left of the primary screen (guaranteed by Apple to be the screen
// at index 0 of NSScreen.screens), Y increasing upward. Every other backend in
// this client — and the ui package they share — works in the opposite space:
// origin at the top-left, Y increasing downward, which is what the xpra
// protocol itself uses. These functions are the one place that conversion
// happens, anchored on referenceHeight, the height of that primary screen.
//
// The anchor has to be the *primary* screen's height, not each individual
// screen's own height: a secondary monitor above or below the primary one
// would otherwise come out at the wrong Y position once flipped, because its
// own height has nothing to do with where its origin sits in the shared,
// global coordinate space.

// cocoaToTopLeft converts a rectangle from Cocoa's bottom-left/Y-up space into
// the top-left/Y-down space every ui.Window and ui.Monitor is expressed in.
func cocoaToTopLeft(x, y, width, height, referenceHeight float64) (tx, ty, tw, th int) {
	return int(x), int(referenceHeight - y - height), int(width), int(height)
}

// topLeftToCocoa is the inverse of cocoaToTopLeft, used whenever this backend
// creates or moves a window from the top-left coordinates the server sends.
func topLeftToCocoa(x, y, width, height int, referenceHeight float64) nsRect {
	return makeRect(
		float64(x),
		referenceHeight-float64(y)-float64(height),
		float64(width),
		float64(height),
	)
}
