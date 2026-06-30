// Package catalog defines the loradex.yaml schema and its validation rules.
// The CLI validates for UX and fast failure; the server re-validates everything.
package catalog

import (
	"os"

	"github.com/goccy/go-yaml"
)

// Catalog mirrors loradex.yaml (published to the server as config.json).
type Catalog struct {
	Name              string   `yaml:"name"`
	Description       string   `yaml:"description"`
	Visibility        string   `yaml:"visibility"`
	BaseModel         string   `yaml:"base_model"`
	Format            string   `yaml:"format"`
	License           string   `yaml:"license"`
	Weights           string   `yaml:"weights"`
	TriggerWords      []string `yaml:"trigger_words"`
	NetworkRank       int      `yaml:"network_rank"`
	NetworkDim        int      `yaml:"network_dim"`
	RecommendedWeight float64  `yaml:"recommended_weight"`
	Tags              []string `yaml:"tags"`
}

// Known enum values. Unknown base_model/format warn (don't fail).
var (
	KnownBaseModels = []string{"flux2-klein", "flux1", "sdxl", "sd15"}
	KnownFormats    = []string{"safetensors", "drawthings", "mlx", "diffusers"}
)

// Load reads and unmarshals a loradex.yaml.
func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Marshal renders the catalog as YAML bytes.
func (c *Catalog) Marshal() ([]byte, error) { return yaml.Marshal(c) }
