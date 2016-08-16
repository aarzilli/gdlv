package nucular

import (
	"image"

	"golang.org/x/image/font"
	"golang.org/x/mobile/event/mouse"

	"github.com/aarzilli/nucular/types"
)

type Heading int

const (
	Up Heading = iota
	Right
	Down
	Left
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func assert(cond bool) {
	if !cond {
		panic("assert!")
	}
}

func assert2(cond bool, reason string) {
	if !cond {
		panic(reason)
	}
}

func triangleFromDirection(r types.Rect, pad_x, pad_y int, direction Heading) (result []image.Point) {
	result = make([]image.Point, 3)
	var w_half int
	var h_half int

	r.W = max(2*pad_x, r.W)
	r.H = max(2*pad_y, r.H)
	r.W = r.W - 2*pad_x
	r.H = r.H - 2*pad_y

	r.X = r.X + pad_x
	r.Y = r.Y + pad_y

	w_half = r.W / 2.0
	h_half = r.H / 2.0

	if direction == Up {
		result[0] = image.Point{r.X + w_half, r.Y}
		result[1] = image.Point{r.X + r.W, r.Y + r.H}
		result[2] = image.Point{r.X, r.Y + r.H}
	} else if direction == Right {
		result[0] = image.Point{r.X, r.Y}
		result[1] = image.Point{r.X + r.W, r.Y + h_half}
		result[2] = image.Point{r.X, r.Y + r.H}
	} else if direction == Down {
		result[0] = image.Point{r.X, r.Y}
		result[1] = image.Point{r.X + r.W, r.Y}
		result[2] = image.Point{r.X + w_half, r.Y + r.H}
	} else {
		result[0] = image.Point{r.X, r.Y + h_half}
		result[1] = image.Point{r.X + r.W, r.Y}
		result[2] = image.Point{r.X + r.W, r.Y + r.H}
	}
	return
}

func minFloat(x, y float64) float64 {
	if x < y {
		return x
	}
	return y
}

func maxFloat(x, y float64) float64 {
	if x > y {
		return x
	}
	return y
}

func clampFloat(i, v, x float64) float64 {
	if v < i {
		v = i
	}
	if v > x {
		v = x
	}
	return v
}

func clampInt(i, v, x int) int {
	if v < i {
		v = i
	}
	if v > x {
		v = x
	}
	return v
}

func saturateFloat(x float64) float64 {
	return maxFloat(0.0, minFloat(1.0, x))
}

func basicWidgetStateControl(state *types.WidgetStates, in *Input, bounds types.Rect) types.WidgetStates {
	if in == nil {
		*state = types.WidgetStateInactive
		return types.WidgetStateInactive
	}

	hovering := in.Mouse.HoveringRect(bounds)

	if *state == types.WidgetStateInactive && hovering {
		*state = types.WidgetStateHovered
	}

	if *state == types.WidgetStateHovered && !hovering {
		*state = types.WidgetStateInactive
	}

	if *state == types.WidgetStateHovered && in.Mouse.HasClickInRect(mouse.ButtonLeft, bounds) {
		*state = types.WidgetStateActive
	}

	if hovering {
		return types.WidgetStateHovered
	} else {
		return types.WidgetStateInactive
	}
}

func shrinkRect(r types.Rect, amount int) types.Rect {
	var res types.Rect
	r.W = max(r.W, 2*amount)
	r.H = max(r.H, 2*amount)
	res.X = r.X + amount
	res.Y = r.Y + amount
	res.W = r.W - 2*amount
	res.H = r.H - 2*amount
	return res
}

func FontHeight(f *types.Face) int {
	return f.Face.Metrics().Ascent.Ceil() + f.Face.Metrics().Descent.Ceil()
}

func FontWidth(f *types.Face, string string) int {
	d := font.Drawer{Face: f.Face}
	return d.MeasureString(string).Ceil()
}

func unify(a types.Rect, b types.Rect) (clip types.Rect) {
	clip.X = max(a.X, b.X)
	clip.Y = max(a.Y, b.Y)
	clip.W = min(a.X+a.W, b.X+b.W) - clip.X
	clip.H = min(a.Y+a.H, b.Y+b.H) - clip.Y
	clip.W = max(0.0, clip.W)
	clip.H = max(0.0, clip.H)
	return
}

func padRect(r types.Rect, pad image.Point) types.Rect {
	r.W = max(r.W, 2*pad.X)
	r.H = max(r.H, 2*pad.Y)
	r.X += pad.X
	r.Y += pad.Y
	r.W -= 2 * pad.X
	r.H -= 2 * pad.Y
	return r
}
