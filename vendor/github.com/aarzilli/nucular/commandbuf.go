package nucular

import (
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/rect"
	"github.com/aarzilli/nucular/style"
)

type drawParams interface {
	Draw(style *style.Style, out *command.Buffer)
}

type frozenWidget struct {
	ws     style.WidgetStates
	bounds rect.Rect
	//drawParams drawParams
}

type widgetBuffer struct {
	win         *Window
	Clip        rect.Rect
	UseClipping bool
	cur         []frozenWidget
	prev        []frozenWidget
}

func (wbuf *widgetBuffer) PrevState(bounds rect.Rect) style.WidgetStates {
	for i := range wbuf.prev {
		if wbuf.prev[i].bounds == bounds {
			return wbuf.prev[i].ws
		}
	}
	return style.WidgetStateInactive
}

func (wbuf *widgetBuffer) Add(ws style.WidgetStates, bounds rect.Rect, drawParams drawParams) {
	if drawParams != nil {
		drawParams.Draw(&wbuf.win.ctx.Style, &wbuf.win.cmds)
	}
	wbuf.cur = append(wbuf.cur, frozenWidget{ws, bounds})
}

// func (wbuf *widgetBuffer) Run(style *Style, out *CommandBuffer) int {
// 	for i := range wbuf.cur {
// 		wbuf.cur[i].drawParams.Draw(style, out)
// 	}
// 	return len(wbuf.cur)
// }

func (wbuf *widgetBuffer) reset() {
	//wbuf.Clip = nk_null_rect
	wbuf.prev = wbuf.cur
	wbuf.cur = []frozenWidget{}
}
