package project

import (
	"fmt"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/catalog"
)

// RenderCatalog produces a commented loradex.yaml from initial values.
func RenderCatalog(c *catalog.Catalog) string {
	tw := func(words []string) string {
		if len(words) == 0 {
			return "[]"
		}
		return "[" + strings.Join(words, ", ") + "]"
	}
	return fmt.Sprintf(`# loradex catalog — edit, then `+"`loradex push`"+`
name: %s          # lowercase, hyphenated (your repo slug)
description: %q                      # one line, <= 280 chars
visibility: %s                  # public | private

base_model: %s             # flux2-klein | flux1 | sdxl | sd15
format: %s                 # safetensors | drawthings | mlx | diffusers
license: %s

weights: %s   # the file `+"`push`"+` uploads
trigger_words: %s                   # e.g. [ohwxman]
network_rank: %d                     # auto-filled by --from when possible
network_dim: %d
recommended_weight: %s             # 0–2
tags: %s                            # e.g. [portrait, photoreal] (lowercase, hyphenated)
`,
		c.Name, c.Description, c.Visibility, c.BaseModel, c.Format, c.License,
		c.Weights, tw(c.TriggerWords), c.NetworkRank, c.NetworkDim,
		trimFloat(c.RecommendedWeight), tw(c.Tags))
}

// RenderReadme produces the README.md template.
func RenderReadme(c *catalog.Catalog) string {
	desc := c.Description
	if desc == "" {
		desc = "<one-line description>"
	}
	trigger := "<trigger>"
	if len(c.TriggerWords) > 0 {
		trigger = c.TriggerWords[0]
	}
	return fmt.Sprintf("# %s\n\n%s\n\n"+
		"## Example outputs\n\n"+
		"<!-- Add preview images under ./samples and they publish with `loradex push --include-samples` -->\n\n"+
		"## Usage\n\n"+
		"Add the trigger word to your prompt and set the LoRA weight to the recommended value.\n\n"+
		"```\nprompt: a portrait of %s, cinematic light\nloradex: %s <weight:%s>\n```\n\n"+
		"## Training notes\n\n"+
		"- Base model: %s · format: %s\n"+
		"- Network rank / dim: %d / %d\n"+
		"- Recommended weight: %s\n",
		c.Name, desc, trigger, c.Name, trimFloat(c.RecommendedWeight),
		c.BaseModel, c.Format, c.NetworkRank, c.NetworkDim, trimFloat(c.RecommendedWeight))
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
