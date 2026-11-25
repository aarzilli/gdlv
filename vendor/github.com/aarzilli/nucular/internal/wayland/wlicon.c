#define _GNU_SOURCE

#include <string.h>
#include <sys/mman.h>
#include <unistd.h>
#include <wayland-client.h>

#include "wlicon.h"
#include "xdg-toplevel-icon-v1-client-protocol.h"

#define MAX_SIZES 16
#define MAX_TOPLEVELS 16

struct icon_ctx {
	struct wl_display *dpy;
	struct wl_event_queue *queue;
	struct wl_shm *shm;
	struct xdg_toplevel_icon_manager_v1 *mgr;
	int32_t sizes[MAX_SIZES];
	int nsizes;
};

// Icon currently set on a toplevel. Both the icon object and its buffer must
// be kept alive until the icon is replaced.
struct icon_state {
	struct xdg_toplevel *toplevel;
	struct xdg_toplevel_icon_v1 *icon;
	struct wl_buffer *buf;
};

// There is only ever one display per process, cache the objects bound to it.
static struct icon_ctx ctx;
static struct icon_state state[MAX_TOPLEVELS];

static void registry_global(void *data, struct wl_registry *reg, uint32_t name, const char *iface, uint32_t version) {
	struct icon_ctx *c = data;
	if (strcmp(iface, wl_shm_interface.name) == 0) {
		c->shm = wl_registry_bind(reg, name, &wl_shm_interface, 1);
	} else if (strcmp(iface, xdg_toplevel_icon_manager_v1_interface.name) == 0) {
		c->mgr = wl_registry_bind(reg, name, &xdg_toplevel_icon_manager_v1_interface, 1);
	}
}

static void registry_global_remove(void *data, struct wl_registry *reg, uint32_t name) {
}

static const struct wl_registry_listener registry_listener = {
	registry_global,
	registry_global_remove,
};

static void manager_icon_size(void *data, struct xdg_toplevel_icon_manager_v1 *mgr, int32_t size) {
	struct icon_ctx *c = data;
	if (c->nsizes < MAX_SIZES) {
		c->sizes[c->nsizes++] = size;
	}
}

static void manager_done(void *data, struct xdg_toplevel_icon_manager_v1 *mgr) {
}

static const struct xdg_toplevel_icon_manager_v1_listener manager_listener = {
	manager_icon_size,
	manager_done,
};

// geticonctx binds wl_shm and the toplevel icon manager, the first time it is
// called, and returns them. The objects it creates are bound to a private
// event queue: the default queue belongs to gio and is dispatched by it.
static struct icon_ctx *geticonctx(struct wl_display *dpy) {
	if (ctx.dpy == dpy) {
		return (ctx.shm != NULL && ctx.mgr != NULL) ? &ctx : NULL;
	}

	memset(&ctx, 0, sizeof(ctx));
	ctx.dpy = dpy;

	ctx.queue = wl_display_create_queue(dpy);
	if (ctx.queue == NULL) {
		return NULL;
	}

	struct wl_display *wrapper = wl_proxy_create_wrapper(dpy);
	if (wrapper == NULL) {
		return NULL;
	}
	wl_proxy_set_queue((struct wl_proxy *)wrapper, ctx.queue);
	struct wl_registry *reg = wl_display_get_registry(wrapper);
	wl_proxy_wrapper_destroy(wrapper);
	if (reg == NULL) {
		return NULL;
	}

	// everything created by reg inherits its event queue
	wl_registry_add_listener(reg, &registry_listener, &ctx);
	wl_display_roundtrip_queue(dpy, ctx.queue);
	wl_registry_destroy(reg);

	if (ctx.shm == NULL || ctx.mgr == NULL) {
		return NULL;
	}

	// the icon_size events are sent as soon as the manager is bound
	xdg_toplevel_icon_manager_v1_add_listener(ctx.mgr, &manager_listener, &ctx);
	wl_display_roundtrip_queue(dpy, ctx.queue);

	return &ctx;
}

// setstate destroys the icon previously set on toplevel and remembers the new
// one. If there is no room left to remember it the old icon is leaked instead,
// destroying it would reset the icon of its toplevel.
static void setstate(struct xdg_toplevel *toplevel, struct xdg_toplevel_icon_v1 *icon, struct wl_buffer *buf) {
	struct icon_state *slot = NULL;
	for (int i = 0; i < MAX_TOPLEVELS; i++) {
		if (state[i].toplevel == toplevel) {
			// the buffer must outlive the icon it is attached to
			xdg_toplevel_icon_v1_destroy(state[i].icon);
			wl_buffer_destroy(state[i].buf);
			slot = &state[i];
			break;
		}
		if (state[i].toplevel == NULL && slot == NULL) {
			slot = &state[i];
		}
	}
	if (slot == NULL) {
		return;
	}
	slot->toplevel = toplevel;
	slot->icon = icon;
	slot->buf = buf;
}

int nucular_wl_icon_sizes(struct wl_display *dpy, int32_t *sizes, int max) {
	struct icon_ctx *c = geticonctx(dpy);
	if (c == NULL) {
		return -1;
	}
	int n = c->nsizes < max ? c->nsizes : max;
	memcpy(sizes, c->sizes, n * sizeof(int32_t));
	return n;
}

int nucular_wl_icon_set(struct wl_display *dpy, struct xdg_toplevel *toplevel, struct wl_surface *surface, int32_t size, const void *pixels) {
	struct icon_ctx *c = geticonctx(dpy);
	if (c == NULL) {
		return -1;
	}

	int32_t stride = size * 4;
	size_t nbytes = (size_t)stride * (size_t)size;

	int fd = memfd_create("nucular-icon", MFD_CLOEXEC);
	if (fd < 0) {
		return -1;
	}
	if (ftruncate(fd, nbytes) < 0) {
		close(fd);
		return -1;
	}
	void *p = mmap(NULL, nbytes, PROT_READ|PROT_WRITE, MAP_SHARED, fd, 0);
	if (p == MAP_FAILED) {
		close(fd);
		return -1;
	}
	memcpy(p, pixels, nbytes);
	munmap(p, nbytes);

	struct wl_shm_pool *pool = wl_shm_create_pool(c->shm, fd, nbytes);
	close(fd);
	if (pool == NULL) {
		return -1;
	}
	struct wl_buffer *buf = wl_shm_pool_create_buffer(pool, 0, size, size, stride, WL_SHM_FORMAT_ARGB8888);
	wl_shm_pool_destroy(pool);
	if (buf == NULL) {
		return -1;
	}

	struct xdg_toplevel_icon_v1 *icon = xdg_toplevel_icon_manager_v1_create_icon(c->mgr);
	if (icon == NULL) {
		wl_buffer_destroy(buf);
		return -1;
	}
	xdg_toplevel_icon_v1_add_buffer(icon, buf, 1);
	xdg_toplevel_icon_manager_v1_set_icon(c->mgr, toplevel, icon);

	// setting the icon is double buffered, it is applied by the next commit
	wl_surface_commit(surface);
	wl_display_flush(dpy);

	setstate(toplevel, icon, buf);

	return 0;
}
