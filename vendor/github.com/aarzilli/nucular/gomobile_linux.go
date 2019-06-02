// +build linux,!android

package nucular

// The x11 backend of gomobile sucks and unless we paint constantly it will not process X11 events
const gomobileBackendKeepPainting = true
