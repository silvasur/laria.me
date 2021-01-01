package markdown

import (
	"bytes"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	goldmarkHtml "github.com/yuin/goldmark/renderer/html"
)

func Parse(s string) (string, error) {
	markdown := goldmark.New(
		goldmark.WithExtensions(
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
					// html.WithAllClasses(true),
					html.WithClasses(true),
					html.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithRendererOptions(
			goldmarkHtml.WithUnsafe(),
		),
	)

	buf := new(bytes.Buffer)
	if err := markdown.Convert([]byte(s), buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}
