package nucular

import (
	"image"
	"iter"

	"github.com/aarzilli/nucular/rect"

	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/mouse"
)

type mouseButton struct {
	Down       bool
	Clicked    bool
	ClickedPos image.Point
}

type MouseInput struct {
	valid       bool
	clip        rect.Rect
	Buttons     [4]mouseButton
	Pos         image.Point
	Prev        image.Point
	Delta       image.Point
	ScrollDelta float32
}

type KeyboardInput struct {
	events []KeyboardEvent
	/*Keys []key.Event
	Text string*/
}

// KeyboardEvent is a keyboard event or a clipboard transfer event.
type KeyboardEvent struct {
	kind    keyboardEventKind
	key     key.Event
	text    string
	handled bool
}

type Input struct {
	Keyboard KeyboardInput
	Mouse    MouseInput
	/*HasClipboard bool
	Clipboard    string*/

	activateEditor interface{}
	activateWindow *Window
}

func (win *Window) Input() *Input {
	if !win.toplevel() {
		return &Input{}
	}
	win.ctx.Input.Mouse.clip = win.cmds.Clip
	return &win.ctx.Input
}

func (win *Window) scrollwheelInput() *Input {
	if win.ctx.scrollwheelFocus == win.idx {
		return &win.ctx.Input
	}
	return &Input{}
}

func (win *Window) KeyboardOnHover(bounds rect.Rect) KeyboardInput {
	if !win.toplevel() || !win.ctx.Input.Mouse.HoveringRect(bounds) {
		return KeyboardInput{}
	}
	return win.ctx.Input.Keyboard
}

func (i *MouseInput) HasClickInRect(id mouse.Button, b rect.Rect) bool {
	btn := &i.Buttons[id]
	return unify(b, i.clip).Contains(btn.ClickedPos)
}

func (i *MouseInput) IsClickInRect(id mouse.Button, b rect.Rect) bool {
	return i.IsClickDownInRect(id, b, false)
}

func (i *MouseInput) IsClickDownInRect(id mouse.Button, b rect.Rect, down bool) bool {
	btn := &i.Buttons[id]
	return i.HasClickInRect(id, b) && btn.Down == down && btn.Clicked
}

func (i *MouseInput) AnyClickInRect(b rect.Rect) bool {
	return i.IsClickInRect(mouse.ButtonLeft, b) || i.IsClickInRect(mouse.ButtonMiddle, b) || i.IsClickInRect(mouse.ButtonRight, b)
}

func (i *MouseInput) HoveringRect(rect rect.Rect) bool {
	return i.valid && unify(rect, i.clip).Contains(i.Pos)
}

func (i *MouseInput) PrevHoveringRect(rect rect.Rect) bool {
	return i.valid && unify(rect, i.clip).Contains(i.Prev)
}

func (i *MouseInput) Clicked(id mouse.Button, rect rect.Rect) bool {
	if !i.HoveringRect(rect) {
		return false
	}
	return i.IsClickInRect(id, rect)
}

func (i *MouseInput) Down(id mouse.Button) bool {
	return i.Buttons[id].Down
}

func (i *MouseInput) Pressed(id mouse.Button) bool {
	return i.Buttons[id].Down && i.Buttons[id].Clicked
}

func (i *MouseInput) Released(id mouse.Button) bool {
	return !(i.Buttons[id].Down) && i.Buttons[id].Clicked
}

type keyboardEventKind int

const (
	keyboardEventKey keyboardEventKind = iota
	keyboardEventText
	keyboardEventClipboard
)

func (i *KeyboardInput) addClipboard(text string) {
	i.events = append(i.events, KeyboardEvent{kind: keyboardEventClipboard, text: text})
}

func (i *KeyboardInput) addText(text string) {
	if text != "" {
		i.events = append(i.events, KeyboardEvent{kind: keyboardEventText, text: text})
	}
}

func (ki *KeyboardInput) Events() iter.Seq[*KeyboardEvent] {
	return func(yield func(ev *KeyboardEvent) bool) {
		didhandle := false
		for i := range ki.events {
			cont := yield(&ki.events[i])
			if ki.events[i].handled {
				didhandle = true
			}
			if !cont {
				break
			}
		}
		if didhandle {
			// reap handled events
			newevents := ki.events[:0]
			for _, e := range ki.events {
				if !e.handled {
					newevents = append(newevents, e)
				}
			}
			ki.events = newevents
		}
	}
}

// HandleText returns true if this is a text event and marks it as handled.
func (e *KeyboardEvent) HandleText() bool {
	if e.kind == keyboardEventText {
		e.handled = true
		return true
	}
	return false
}

// Text returns the text of this event, must be a text or clipboard event
// that was already marked as handled.
func (e *KeyboardEvent) Text() string {
	if !e.handled {
		return ""
	}
	return e.text
}

// HandleKey returns true if this is a key event matching code and modifiers
// and marks it as handled.
func (e *KeyboardEvent) HandleKey(code key.Code, modifiers key.Modifiers) bool {
	if e.kind == keyboardEventKey && e.key.Code == code && e.key.Modifiers == modifiers {
		e.handled = true
		return true
	}
	return false
}

// HandleKeyModmask returns true if this is a key event matching the key and
// at least one of the modifiers in modmask. Marks it as handled.
func (e *KeyboardEvent) HandleKeyModmask(code key.Code, modmask key.Modifiers) bool {
	if e.kind == keyboardEventKey && e.key.Code == code && e.key.Modifiers&modmask != 0 {
		e.handled = true
		return true
	}
	return false
}

// HandleKeyAny returns true if this is a key event and marks it as handled, unconditionally.
func (e *KeyboardEvent) HandleKeyAny() bool {
	if e.kind == keyboardEventKey {
		e.handled = true
		return true
	}
	return false
}

// HandleKeyRune returns true if this is a key event matching the given rune.
func (e *KeyboardEvent) HandleKeyRune(r rune) bool {
	if e.kind == keyboardEventKey && e.key.Rune == r {
		e.handled = true
		return true
	}
	return false
}

// Key returns the key press of a key event, that has already been marked as
// handled.
func (e *KeyboardEvent) Key() key.Event {
	if !e.handled {
		return key.Event{}
	}
	return e.key
}

// Unhandle marks this event as not handled.
func (e *KeyboardEvent) Unhandle() {
	e.handled = false
}

// HandleClipboard returns true if this is a clipboard event and marks it as
// handled.
func (e *KeyboardEvent) HandleClipboard() bool {
	if e.kind == keyboardEventClipboard {
		e.handled = true
		return true
	}
	return false
}

func (win *Window) inputMaybe(widgetValid bool) *Input {
	if widgetValid && win.toplevel() && win.flags&windowEnabled != 0 {
		win.ctx.Input.Mouse.clip = win.cmds.Clip
		return &win.ctx.Input
	}
	return &Input{}
}

func (win *Window) toplevel() bool {
	if win.moving {
		return false
	}
	if win.ctx.dockedWindowFocus != 0 && win.idx == win.ctx.dockedWindowFocus {
		return true
	}
	return win.idx == win.ctx.floatWindowFocus
}
