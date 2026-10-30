package media

import (
	"encoding/json"
	"os"
	"testing"
)

// SHARP_DIMS : mesures produites par sharp (resize + withoutEnlargement) sur une
// grille de tailles d'entrée ; targetDims doit retrouver les mêmes dimensions.
func TestTargetDimsAgainstSharp(t *testing.T) {
	path := os.Getenv("SHARP_DIMS")
	if path == "" {
		t.Skip("SHARP_DIMS non défini")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		W, H, Ow, Oh int
		Type         string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		_, _, cw, ch := targetDims(c.W, c.H, imageConfigs[c.Type])
		if cw != c.Ow || ch != c.Oh {
			t.Errorf("%s %dx%d : sharp %dx%d, Go %dx%d", c.Type, c.W, c.H, c.Ow, c.Oh, cw, ch)
		}
	}
	t.Logf("%d cas comparés", len(cases))
}

func TestMagicBytes(t *testing.T) {
	cases := map[string]string{
		"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01": "image/jpeg",
		"\x89PNG\r\n\x1a\n\x00\x00\x00\r":      "image/png",
		"GIF89a\x01\x00\x01\x00\x00\x00":       "image/gif",
		"RIFF\x00\x00\x00\x00WEBPVP8 ":         "image/webp",
		"\x00\x00\x00\x1cftypavif\x00\x00":     "image/avif",
		"\x00\x00\x00\x1cftypmp42\x00\x00":     "",
		"<svg xmlns='x'></svg>   ":             "",
		"short":                                "",
	}
	for in, want := range cases {
		if got := ValidateMagicBytes([]byte(in)); got != want {
			t.Errorf("%q : %q, want %q", in, got, want)
		}
	}
}
