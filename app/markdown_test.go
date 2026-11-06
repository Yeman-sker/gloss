package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Strong text and code blocks, as the panel shows Markdown: the dump is a
// picture to look at, with GLOSS_DUMP set to a directory.
func TestMarkdownBoldAndCodeCopy(t *testing.T) {
	if err := registerFonts(); err != nil {
		t.Fatal(err)
	}
	var p panel
	p.md, p.netDone = "普通的中文和**加粗的中文**，plain and **bold English**。\n\n```go\nfmt.Println(1)\n```", true
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(16).Background(colorSurface).Fill().Children(func() {
			p.markdownView(c)
		})
	}, 520, 220)
	tt.SetScale(2)
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "fmt.Println(1)" && tt.Clipboard() != "fmt.Println(1)\n" {
		t.Errorf("clipboard %q", tt.Clipboard())
	}
	if !tt.HasText("已复制") {
		t.Errorf("texts %q", tt.Texts())
	}
	if dir := os.Getenv("GLOSS_DUMP"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "bold.png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
}
