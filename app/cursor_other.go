//go:build !darwin

package app

import "github.com/egoist/mygo/ui"

// Elsewhere mygo's cursors work whether or not the app is active.
func allowBackgroundCursor() {}

func setCursor(ui.Cursor) {}
