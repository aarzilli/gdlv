#ifndef NUCULAR_WLICON_H
#define NUCULAR_WLICON_H

#include <stdint.h>
#include <wayland-client.h>

struct xdg_toplevel;

// nucular_wl_icon_sizes writes the icon sizes preferred by the compositor to
// sizes, which must have room for max elements, and returns the number of
// sizes written (possibly 0). Returns -1 if the compositor does not support
// the xdg-toplevel-icon-v1 protocol.
int nucular_wl_icon_sizes(struct wl_display *dpy, int32_t *sizes, int max);

// nucular_wl_icon_set sets the icon of toplevel to the size×size image in
// pixels, which must be in ARGB8888 format with premultiplied alpha.
// Returns 0 on success, -1 on failure.
int nucular_wl_icon_set(struct wl_display *dpy, struct xdg_toplevel *toplevel,
                        struct wl_surface *surface, int32_t size,
                        const void *pixels);

#endif
