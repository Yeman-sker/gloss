package app

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

// mygo tracks the pointer only while its app is active, and Gloss never is:
// it leaves the keyboard with the app the user is in. So the panel sets the
// cursor itself, with the window server's leave to do so from the background.
// SetsCursorInBackground is private API; without it the cursor stays as the
// app underneath left it, and nothing else breaks.
func allowBackgroundCursor() {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY)
	if err != nil {
		return
	}
	var connection func() int32
	var setProperty func(owner, target int32, key, value uintptr) int32
	for name, fn := range map[string]any{"_CGSDefaultConnection": &connection, "CGSSetConnectionProperty": &setProperty} {
		sym, err := purego.Dlsym(lib, name)
		if err != nil {
			return
		}
		purego.RegisterFunc(fn, sym)
	}
	name := []byte("SetsCursorInBackground\x00")
	key := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), uintptr(unsafe.Pointer(&name[0])))
	yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
	c := connection()
	setProperty(c, c, uintptr(key), uintptr(yes))
}

// setCursor holds the cursor between frames. AppKit also restores mygo's on a
// click, so the elements carry the same one: see panel.track.
func setCursor(c ui.Cursor) {
	name := "arrowCursor"
	switch c {
	case ui.CursorPointer:
		name = "pointingHandCursor"
	case ui.CursorText:
		name = "IBeamCursor"
	case ui.CursorGrab:
		name = "openHandCursor"
	case ui.CursorGrabbing:
		name = "closedHandCursor"
	}
	objc.ID(objc.GetClass("NSCursor")).Send(objc.RegisterName(name)).Send(objc.RegisterName("set"))
}
