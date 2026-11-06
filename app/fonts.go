package app

import (
	"embed"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// fonts are the faces of docs/decisions/0006-bundled-fonts.md, built by
// scripts/subset_fonts.py.
//
//go:embed fonts/*.ttf
var fonts embed.FS

// registerFonts adds the bundled faces under the names of tokens.go. A
// bold face shares its family's name, so that strong text finds it.
func registerFonts() error {
	for file, family := range map[string]string{
		"cjk.ttf":        "Gloss CJK",
		"cjk-bold.ttf":   "Gloss CJK",
		"cjk-quotes.ttf": "Gloss Quotes",
		"geist.ttf":      "Gloss Sans",
		"geist-bold.ttf": "Gloss Sans",
		"geist-mono.ttf": "Gloss Mono",
		"phonetic.ttf":   "Gloss Phonetic",
	} {
		data, err := fonts.ReadFile("fonts/" + file)
		if err != nil {
			return err
		}
		if err := ui.RegisterFont(data, family); err != nil {
			return fmt.Errorf("font %s: %w", file, err)
		}
	}
	return nil
}
