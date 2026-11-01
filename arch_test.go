package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The layering of docs/ARCHITECTURE.md, enforced: a package may not depend on
// any of these, directly or not.
var forbidden = map[string][]string{
	"gloss/core":             {"github.com/egoist/mygo", "gloss/app", "gloss/capture", "gloss/providers"},
	"gloss/capture":          {"github.com/egoist/mygo", "gloss/app", "gloss/core", "gloss/providers"},
	"gloss/providers/demo":   {"github.com/egoist/mygo", "gloss/app", "gloss/capture"},
	"gloss/providers/openai": {"github.com/egoist/mygo", "gloss/app", "gloss/capture"},
}

func TestLayers(t *testing.T) {
	for pkg, bad := range forbidden {
		out, err := exec.Command("go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, b := range bad {
				if dep == b || strings.HasPrefix(dep, b+"/") {
					t.Errorf("%s depends on %s", pkg, dep)
				}
			}
		}
	}
}
