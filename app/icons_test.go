package app

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gloss/providers/prompt"
)

var undrawnDash = regexp.MustCompile(`stroke-dasharray="([\d.]+)" stroke-dashoffset="([\d.]+)"`)

// Every baked icon parses at every frame, with the same count, and no filter
// slipped through: mygo would drop it silently.
func TestIconsBaked(t *testing.T) {
	raw, err := os.ReadFile("../design/icons/gloss-icons.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, m := range manifest {
		text, err := readIconFile(m.ID)
		if err != nil {
			t.Errorf("%s: %v", m.ID, err)
			continue
		}
		frames := strings.Split(text, "\n")
		if len(frames) != iconFrames {
			t.Errorf("%s: %d frames, want %d", m.ID, len(frames), iconFrames)
		}
		for i, f := range frames {
			if strings.Contains(f, "<filter") || strings.Contains(f, "filter=") {
				t.Errorf("%s frame %d keeps a filter", m.ID, i)
			}
			// A dash yet to start leaves a dot in mygo, where a browser draws nothing.
			for _, d := range undrawnDash.FindAllStringSubmatch(f, -1) {
				if d[1] == d[2] {
					t.Errorf("%s frame %d keeps a stroke that has not started", m.ID, i)
				}
			}
			if _, err := ui.ParseSVG([]byte(f)); err != nil {
				t.Errorf("%s frame %d: %v", m.ID, i, err)
			}
		}
	}
}

// Every built-in feature has an icon of its own, and any other falls back.
func TestFeaturesHaveIcons(t *testing.T) {
	specs, err := prompt.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		id := featureIcon(s.Descriptor.ID)
		if id == iconUnknownFeature {
			t.Errorf("%s has no icon", s.Descriptor.ID)
		}
		if loadIcon(id) == nil {
			t.Errorf("%s: no file for %s", s.Descriptor.ID, id)
		}
	}
	if featureIcon("some-plugin") != iconUnknownFeature || loadIcon(iconUnknownFeature) == nil {
		t.Error("an unknown feature must fall back to an icon that exists")
	}
}
