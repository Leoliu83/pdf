package pdf

import (
	"sort"
	"strings"
)

type TextBlock struct {
	Position int64
	Content  []Text
}

type TextBlockBuilder interface {
	Write(t Rune)
	Build() []*TextBlock
}

var _ TextBlockBuilder = &rowBuilder{}

type rowBuilder struct {
	blocks []*TextBlock
	index  map[int64]*TextBlock // key: float64, value: *TextBlock
}

func NewRowBuilder() TextBlockBuilder {
	return &rowBuilder{
		blocks: make([]*TextBlock, 0, 10),
		index:  make(map[int64]*TextBlock),
	}
}

func (rb *rowBuilder) Write(r Rune) {
	key := int64(r.Y)
	tb, ok := rb.index[key]
	if !ok {
		tb = &TextBlock{
			Position: key,
			Content:  TextHorizontal{},
		}
		rb.index[key] = tb
		rb.blocks = append(rb.blocks, tb)
	}
	tb.Content = append(tb.Content, Text{
		r.Font, r.FontSize, r.X, r.Y, r.W, string(r.R),
	})
}

func (rb *rowBuilder) Build() []*TextBlock {
	for _, block := range rb.blocks {
		sort.Stable(TextHorizontal(block.Content))
	}
	sort.SliceStable(
		rb.blocks, func(i, j int) bool {
			return rb.blocks[i].Position > rb.blocks[j].Position
		},
	)
	return rb.blocks
}

type Rune struct {
	Font     string  // the font used
	FontSize float64 // the font size, in points (1/72 of an inch)
	X        float64 // the X coordinate, in points, increasing left to right
	Y        float64 // the Y coordinate, in points, increasing bottom to top
	W        float64 // the width of the text, in points
	R        rune    // the actual rune, zero Heap Allocation
}

// Content returns the page's content.
func (p Page) WalkTextBlocks(builder TextBlockBuilder) {
	// Handle in case the content page is empty
	if p.V.IsNull() || p.V.Key("Contents").Kind() == Null {
		return
	}
	strm := p.V.Key("Contents")
	var enc TextEncoding = &nopEncoder{}

	var g = gstate{
		Th:  1,
		CTM: ident,
	}

	showText := func(s string, builder TextBlockBuilder) {
		n := 0
		decoded := enc.Decode(s)
		// log.Println(decoded)
		for _, ch := range decoded {
			var w0 float64
			if n < len(s) {
				w0 = g.Tf.Width(int(s[n]))
			}
			n++
			f := g.Tf.BaseFont()
			if i := strings.Index(f, "+"); i >= 0 {
				f = f[i+1:]
			}

			Trm := matrix{{g.Tfs * g.Th, 0, 0}, {0, g.Tfs, 0}, {0, g.Trise, 1}}.mul(g.Tm).mul(g.CTM)
			builder.Write(Rune{f, Trm[0][0], Trm[2][0], Trm[2][1], w0 / 1000 * Trm[0][0], ch})

			tx := w0/1000*g.Tfs + g.Tc
			tx *= g.Th
			g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
		}
	}

	var gstack []gstate
	Interpret(strm, func(stk *Stack, op string) {
		args := popArgs(stk)
		switch op {
		default:
			// if DebugOn {
			// 	fmt.Println(op, args)
			// }
			return

		case "cm": // update g.CTM
			if len(args) != 6 {
				panic("bad g.Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.CTM = m.mul(g.CTM)

		case "gs": // set parameters from graphics state resource
			//gs := p.Resources().Key("ExtGState").Key(args[0].Name())
			//font := gs.Key("Font")
			//if font.Kind() == Array && font.Len() == 2 {
			// if DebugOn {
			// 	fmt.Println("FONT", font)
			// }
			//}

		case "f": // fill
		case "g": // setgray
		case "l": // lineto
		case "m": // moveto

		case "cs": // set colorspace non-stroking
		case "scn": // set color non-stroking

		case "re": // append rectangle to path
			if len(args) != 4 {
				panic("bad re")
			}
		case "q": // save graphics state
			gstack = append(gstack, g)

		case "Q": // restore graphics state
			n := len(gstack) - 1
			g = gstack[n]
			gstack = gstack[:n]

		case "BT": // begin text (reset text matrix and line matrix)
			g.Tm = ident
			g.Tlm = g.Tm

		case "ET": // end text

		case "T*": // move to start of next line
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tc": // set character spacing
			if len(args) != 1 {
				panic("bad g.Tc")
			}
			g.Tc = args[0].Float64()

		case "TD": // move text position and set leading
			if len(args) != 2 {
				panic("bad Td")
			}
			g.Tl = -args[1].Float64()
			fallthrough
		case "Td": // move text position
			if len(args) != 2 {
				panic("bad Td")
			}
			tx := args[0].Float64()
			ty := args[1].Float64()
			x := matrix{{1, 0, 0}, {0, 1, 0}, {tx, ty, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm

		case "Tf": // set text font and size
			if len(args) != 2 {
				panic("bad TL")
			}
			fonts := p.fontCache()
			f := args[0].Name()
			if font, ok := fonts[f]; ok {
				g.Tf = *font
			} else {
				g.Tf = p.Font(f)
			}
			enc = g.Tf.Encoder()
			if enc == nil {
				if DebugOn {
					println("no cmap for", f)
				}
				enc = &nopEncoder{}
			}
			g.Tfs = args[1].Float64()

		case "\"": // set spacing, move to next line, and show text
			if len(args) != 3 {
				panic("bad \" operator")
			}
			g.Tw = args[0].Float64()
			g.Tc = args[1].Float64()
			args = args[2:]
			fallthrough
		case "'": // move to next line and show text
			if len(args) != 1 {
				panic("bad ' operator")
			}
			x := matrix{{1, 0, 0}, {0, 1, 0}, {0, -g.Tl, 1}}
			g.Tlm = x.mul(g.Tlm)
			g.Tm = g.Tlm
			fallthrough
		case "Tj": // show text
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			showText(args[0].RawString(), builder)

		case "TJ": // show text, allowing individual glyph positioning
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == String {
					showText(x.RawString(), builder)
				} else {
					tx := -x.Float64() / 1000 * g.Tfs * g.Th
					g.Tm = matrix{{1, 0, 0}, {0, 1, 0}, {tx, 0, 1}}.mul(g.Tm)
				}
			}
			// showText("\n", do)

		case "TL": // set text leading
			if len(args) != 1 {
				panic("bad TL")
			}
			g.Tl = args[0].Float64()

		case "Tm": // set text matrix and line matrix
			if len(args) != 6 {
				panic("bad g.Tm")
			}
			var m matrix
			for i := 0; i < 6; i++ {
				m[i/2][i%2] = args[i].Float64()
			}
			m[2][2] = 1
			g.Tm = m
			g.Tlm = m

		case "Tr": // set text rendering mode
			if len(args) != 1 {
				panic("bad Tr")
			}
			g.Tmode = int(args[0].Int64())

		case "Ts": // set text rise
			if len(args) != 1 {
				panic("bad Ts")
			}
			g.Trise = args[0].Float64()

		case "Tw": // set word spacing
			if len(args) != 1 {
				panic("bad g.Tw")
			}
			g.Tw = args[0].Float64()

		case "Tz": // set horizontal text scaling
			if len(args) != 1 {
				panic("bad Tz")
			}
			g.Th = args[0].Float64() / 100
		}
	})
}
