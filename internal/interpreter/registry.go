// Package interpreter is the registry of caption ("interpreter") models —
// vision-language models that describe dataset images before training. It
// mirrors internal/basemodel: a curated list plus user-cataloged custom entries,
// an on-disk store under <home>/interpreters, and a HuggingFace fetcher.
package interpreter

import (
	"sort"

	"github.com/keeandrews/loradex-cli/internal/config"
)

// DefaultID is the interpreter chosen when none is configured.
const DefaultID = "qwen3-vl-4b"

// Entry is one caption model — built-in or custom.
type Entry struct {
	ID     string  // unique slug, also the --interpreter id
	Name   string  // display name
	Repo   string  // HuggingFace repo id
	SizeGB float64 // approximate download size
	Gated  bool    // requires HuggingFace auth
	Custom bool    // user-cataloged
	Desc   string  // one-line description
}

var builtin = []Entry{
	{
		ID: "qwen3-vl-4b", Name: "Qwen3-VL 4B Instruct", Repo: "Qwen/Qwen3-VL-4B-Instruct",
		SizeGB: 9, Desc: "Qwen3-VL 4B — detailed image captions, fits Apple Silicon (default)",
	},
	{
		ID: "qwen3-vl-8b", Name: "Qwen3-VL 8B Instruct", Repo: "Qwen/Qwen3-VL-8B-Instruct",
		SizeGB: 17, Desc: "Qwen3-VL 8B — higher-quality captions, more memory",
	},
	{
		ID: "qwen2.5-vl-7b", Name: "Qwen2.5-VL 7B Instruct", Repo: "Qwen/Qwen2.5-VL-7B-Instruct",
		SizeGB: 17, Desc: "Qwen2.5-VL 7B — the captioner Draw Things uses",
	},
	{
		ID: "qwen2.5-vl-3b", Name: "Qwen2.5-VL 3B Instruct", Repo: "Qwen/Qwen2.5-VL-3B-Instruct",
		SizeGB: 7, Desc: "Qwen2.5-VL 3B — smallest, fastest",
	},
}

// Builtin returns a copy of the curated registry.
func Builtin() []Entry {
	out := make([]Entry, len(builtin))
	copy(out, builtin)
	return out
}

// All merges built-ins with the user's custom interpreters from config.
func All() ([]Entry, error) {
	f, err := config.Load()
	if err != nil {
		return nil, err
	}
	return merge(builtin, f.CustomInterpreters), nil
}

func merge(base []Entry, custom []config.CustomModel) []Entry {
	byID := map[string]Entry{}
	order := []string{}
	for _, e := range base {
		if _, ok := byID[e.ID]; !ok {
			order = append(order, e.ID)
		}
		byID[e.ID] = e
	}
	for _, c := range custom {
		if c.ID == "" || c.Repo == "" {
			continue
		}
		if _, ok := byID[c.ID]; !ok {
			order = append(order, c.ID)
		}
		byID[c.ID] = Entry{
			ID: c.ID, Name: orStr(c.Name, c.ID), Repo: c.Repo,
			SizeGB: c.SizeGB, Gated: c.Gated, Custom: true, Desc: "custom",
		}
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Find returns the entry with the given id, if any.
func Find(id string) (Entry, bool) {
	all, err := All()
	if err != nil {
		return Entry{}, false
	}
	for _, e := range all {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
