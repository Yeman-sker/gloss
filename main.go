// Gloss is a light desktop app that translates and explains selected text.
// Everything happens in core and app; start reading at docs/ARCHITECTURE.md.
package main

import (
	"log"

	"gloss/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
