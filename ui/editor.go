package ui

import (
	"github.com/gdamore/tcell"
)

// editor is the text field where the user writes messages and commands.
type editor struct {
	// text contains the written runes. An empty slice means no text is written.
	text [][]rune

	lineIdx int

	// textWidth[i] contains the width of string(text[:i]). Therefore
	// len(textWidth) is always strictly greater than 0 and textWidth[0] is
	// always 0.
	textWidth []int

	// cursorIdx is the index in text of the placement of the cursor, or is
	// equal to len(text) if the cursor is at the end.
	cursorIdx int

	// offsetIdx is the number of elements of text that are skipped when
	// rendering.
	offsetIdx int

	// width is the width of the screen.
	width int
}

func newEditor(width int) editor {
	return editor{
		text:      [][]rune{{}},
		textWidth: []int{0},
		width:     width,
	}
}

func (e *editor) Resize(width int) {
	if width < e.width {
		e.cursorIdx = 0
		e.offsetIdx = 0
	}
	e.width = width
}

func (e *editor) IsCommand() bool {
	return len(e.text[e.lineIdx]) != 0 && e.text[e.lineIdx][0] == '/'
}

func (e *editor) TextLen() int {
	return len(e.text[e.lineIdx])
}

func (e *editor) PutRune(r rune) {
	e.text[e.lineIdx] = append(e.text[e.lineIdx], ' ')
	copy(e.text[e.lineIdx][e.cursorIdx+1:], e.text[e.lineIdx][e.cursorIdx:])
	e.text[e.lineIdx][e.cursorIdx] = r

	rw := runeWidth(r)
	tw := e.textWidth[len(e.textWidth)-1]
	e.textWidth = append(e.textWidth, tw+rw)
	for i := e.cursorIdx + 1; i < len(e.textWidth); i++ {
		e.textWidth[i] = rw + e.textWidth[i-1]
	}

	e.Right()
}

func (e *editor) RemRune() (ok bool) {
	ok = 0 < e.cursorIdx
	if !ok {
		return
	}
	e.remRuneAt(e.cursorIdx - 1)
	e.Left()
	return
}

func (e *editor) RemRuneForward() (ok bool) {
	ok = e.cursorIdx < len(e.text)
	if !ok {
		return
	}
	e.remRuneAt(e.cursorIdx)
	return
}

func (e *editor) remRuneAt(idx int) {
	// TODO avoid looping twice
	rw := e.textWidth[idx+1] - e.textWidth[idx]
	for i := idx + 1; i < len(e.textWidth); i++ {
		e.textWidth[i] -= rw
	}
	copy(e.textWidth[idx+1:], e.textWidth[idx+2:])
	e.textWidth = e.textWidth[:len(e.textWidth)-1]

	copy(e.text[e.lineIdx][idx:], e.text[e.lineIdx][idx+1:])
	e.text[e.lineIdx] = e.text[e.lineIdx][:len(e.text[e.lineIdx])-1]
}

func (e *editor) Flush() (content string) {
	content = string(e.text[e.lineIdx])
	if len(e.text[len(e.text)-1]) == 0 {
		e.lineIdx = len(e.text) - 1
	} else {
		e.lineIdx = len(e.text)
		e.text = append(e.text, []rune{})
	}
	e.textWidth = e.textWidth[:1]
	e.cursorIdx = 0
	e.offsetIdx = 0
	return
}

func (e *editor) Right() {
	if e.cursorIdx == len(e.text[e.lineIdx]) {
		return
	}
	e.cursorIdx++
	if e.width <= e.textWidth[e.cursorIdx]-e.textWidth[e.offsetIdx] {
		e.offsetIdx += 16
		max := len(e.text[e.lineIdx]) - 1
		if max < e.offsetIdx {
			e.offsetIdx = max
		}
	}
}

func (e *editor) Left() {
	if e.cursorIdx == 0 {
		return
	}
	e.cursorIdx--
	if e.cursorIdx <= e.offsetIdx {
		e.offsetIdx -= 16
		if e.offsetIdx < 0 {
			e.offsetIdx = 0
		}
	}
}

func (e *editor) Home() {
	e.cursorIdx = 0
	e.offsetIdx = 0
}

func (e *editor) End() {
	e.cursorIdx = len(e.text[e.lineIdx])
	for e.width < e.textWidth[e.cursorIdx]-e.textWidth[e.offsetIdx]+16 {
		e.offsetIdx++
	}
}

func (e *editor) Up() {
	if e.lineIdx == 0 {
		return
	}
	e.lineIdx--
	e.computeTextWidth()
	e.cursorIdx = 0
	e.offsetIdx = 0
	e.End()
}

func (e *editor) Down() {
	if e.lineIdx == len(e.text)-1 {
		if len(e.text[e.lineIdx]) == 0 {
			return
		}
		e.Flush()
		return
	}
	e.lineIdx++
	e.computeTextWidth()
	e.cursorIdx = 0
	e.offsetIdx = 0
	e.End()
}

func (e *editor) computeTextWidth() {
	e.textWidth = e.textWidth[:1]
	rw := 0
	for _, r := range e.text[e.lineIdx] {
		rw += runeWidth(r)
		e.textWidth = append(e.textWidth, rw)
	}
}

func (e *editor) Draw(screen tcell.Screen, y int) {
	st := tcell.StyleDefault

	x := 0
	i := e.offsetIdx

	for i < len(e.text[e.lineIdx]) && x < e.width {
		r := e.text[e.lineIdx][i]
		screen.SetContent(x, y, r, nil, st)
		x += runeWidth(r)
		i++
	}

	for x < e.width {
		screen.SetContent(x, y, ' ', nil, st)
		x++
	}

	curStart := e.textWidth[e.cursorIdx] - e.textWidth[e.offsetIdx]
	curEnd := curStart + 1
	if e.cursorIdx+1 < len(e.textWidth) {
		curEnd = e.textWidth[e.cursorIdx+1] - e.textWidth[e.offsetIdx]
	}
	for x := curStart; x < curEnd; x++ {
		screen.ShowCursor(x, y)
	}
}
