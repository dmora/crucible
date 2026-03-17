package common

import (
	"bytes"
	"image/color"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/dmora/crucible/internal/ui/styles"
)

// SyntaxHighlight applies syntax highlighting to the given source code based
// on the file name and background color. It returns the highlighted code as a
// string.
func SyntaxHighlight(st *styles.Styles, source, fileName string, bg color.Color) (string, error) {
	// Determine the language lexer to use
	l := lexers.Match(fileName)
	if l == nil {
		l = lexers.Analyse(source)
	}
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)

	// Get the formatter
	f := formatters.Get("terminal16m")
	if f == nil {
		f = formatters.Fallback
	}

	style := chroma.MustNewStyle("crucible", st.ChromaTheme())

	// Modify the style to use the provided background (if set).
	builder := style.Builder()
	if bg != nil {
		builder = builder.Transform(
			func(t chroma.StyleEntry) chroma.StyleEntry {
				r, g, b, _ := bg.RGBA()
				t.Background = chroma.NewColour(uint8(r>>8&0xFF), uint8(g>>8&0xFF), uint8(b>>8&0xFF)) //nolint:gosec // RGBA returns pre-multiplied alpha; >>8 always fits uint8
				return t
			},
		)
	}
	s, err := builder.Build()
	if err != nil {
		s = chromastyles.Fallback
	}

	// Tokenize and format
	it, err := l.Tokenise(nil, source)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = f.Format(&buf, s, it)
	return buf.String(), err
}
