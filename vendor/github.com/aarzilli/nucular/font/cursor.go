package font

type Cursor uint8

const (
	DefaultCursor Cursor = iota
	NoCursor
	TextCursor
	PointerCursor
	ProgressCursor
	VerticalResizeCursor
	HorizontalResizeCursor
	ResizeCursor
)
