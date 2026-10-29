// Command analyzer decodes captured frontend bytes using an independent VT.
// Its separate module deliberately keeps emulator dependencies out of the app.
package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/vt"
)

type frame struct {
	Data       []byte
	Rows, Cols int
}
type request struct {
	Rows, Cols int
	Frames     []frame
	Points     [][2]int
}
type cell struct {
	Text   string
	Width  int
	FG, BG string
	Attrs  uint8
}
type state struct {
	Rows, Cols, X, Y int
	Alt, Hidden      bool
	Lines            []string
	Cells            []cell
}

func colorName(c color.Color) string {
	if c == nil {
		return "default"
	}
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("%04x/%04x/%04x/%04x", r, g, b, a)
}
func main() {
	var in request
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		panic(err)
	}
	if in.Rows < 1 || in.Cols < 1 || in.Rows > 200 || in.Cols > 300 {
		panic("invalid dimensions")
	}
	e := vt.NewEmulator(in.Cols, in.Rows)
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, e); close(drained) }()
	for _, f := range in.Frames {
		if f.Rows != 0 || f.Cols != 0 {
			if f.Rows < 1 || f.Cols < 1 || f.Rows > 200 || f.Cols > 300 {
				panic("invalid resize")
			}
			e.Resize(f.Cols, f.Rows)
		}
		if _, err := e.Write(f.Data); err != nil {
			panic(err)
		}
	}
	pos := e.CursorPosition()
	out := state{Rows: e.Height(), Cols: e.Width(), X: pos.X, Y: pos.Y, Alt: e.IsAltScreen(), Hidden: e.CursorHidden()}
	for y := 0; y < out.Rows; y++ {
		var line strings.Builder
		for x := 0; x < out.Cols; x++ {
			c := e.CellAt(x, y)
			if c != nil {
				line.WriteString(c.Content)
			}
		}
		out.Lines = append(out.Lines, line.String())
	}
	for _, p := range in.Points {
		c := e.CellAt(p[0], p[1])
		if c == nil {
			panic("point outside screen")
		}
		out.Cells = append(out.Cells, cell{Text: c.Content, Width: c.Width, FG: colorName(c.Style.Fg), BG: colorName(c.Style.Bg), Attrs: uint8(c.Style.Attrs)})
	}
	if err := e.Close(); err != nil {
		panic(err)
	}
	<-drained
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
