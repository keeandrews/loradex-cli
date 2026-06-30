package cmd

import (
	"testing"

	"github.com/keeandrews/loradex-cli/internal/basemodel"
)

func TestResolveMenuChoice(t *testing.T) {
	all := []basemodel.Entry{{ID: "flux1"}, {ID: "flux2-klein"}, {ID: "sdxl"}}
	ok := map[string]string{
		"2":                               "flux2-klein",
		"flux2-klein":                     "flux2-klein",
		"loradex models pull flux2-klein": "flux2-klein",
		"pull sdxl":                       "sdxl",
		"download flux1":                  "flux1",
	}
	for in, want := range ok {
		e, found := resolveMenuChoice(in, all)
		if !found || e.ID != want {
			t.Errorf("resolveMenuChoice(%q) = %q,%v; want %q", in, e.ID, found, want)
		}
	}
	for _, bad := range []string{"nope", "9", "0", "", "pull does-not-exist"} {
		if _, found := resolveMenuChoice(bad, all); found {
			t.Errorf("resolveMenuChoice(%q) should not resolve", bad)
		}
	}
}
