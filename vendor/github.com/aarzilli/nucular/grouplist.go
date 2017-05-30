package nucular

type GroupList struct {
	w   *Window
	num int

	idx        int
	scrollbary int
	done       bool
}

// GroupListStart starts a scrollable list of <num> rows of <height> height
func GroupListStart(w *Window, num int, name string, flags WindowFlags) (GroupList, *Window) {
	var gl GroupList
	gl.w = w.GroupBegin(name, flags)
	gl.num = num
	gl.idx = -1
	gl.scrollbary = gl.w.Scrollbar.Y

	return gl, gl.w
}

func (gl *GroupList) Next() bool {
	gl.idx++
	if gl.idx >= gl.num {
		if !gl.done {
			gl.done = true
			if gl.scrollbary != gl.w.Scrollbar.Y {
				gl.w.Scrollbar.Y = gl.scrollbary
				gl.w.Master().Changed()
			}
			gl.w.GroupEnd()
		}
		return false
	}
	return true
}

func (gl *GroupList) Index() int {
	return gl.idx
}

func (gl *GroupList) Center() {
	if above, below := gl.w.Invisible(); above || below {
		gl.scrollbary = gl.w.At().Y - gl.w.Bounds.H/2
		if gl.scrollbary < 0 {
			gl.scrollbary = 0
		}
	}
}
