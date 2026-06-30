package basemodel

import (
	"fmt"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/ref"
)

// KnownArchs are the architectures the trainer understands. Custom models may
// use one of these to inherit the matching training profile, or "other".
var KnownArchs = []string{"flux2", "flux1", "sdxl", "sd15", "other"}

// ValidateCustom checks a user-cataloged model before it is persisted.
func ValidateCustom(c config.CustomModel) error {
	if err := ref.ValidateSlug(c.ID); err != nil {
		return fmt.Errorf("id %q: %w", c.ID, err)
	}
	if (c.Repo == "") == (c.URL == "") {
		return fmt.Errorf("provide exactly one of repo or url")
	}
	if c.URL != "" && !strings.HasPrefix(strings.ToLower(c.URL), "https://") {
		return fmt.Errorf("url must be https://")
	}
	if c.Arch != "" {
		ok := false
		for _, a := range KnownArchs {
			if a == c.Arch {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("arch %q is not one of %s", c.Arch, strings.Join(KnownArchs, ", "))
		}
	}
	for _, b := range builtin {
		if b.ID == c.ID {
			return fmt.Errorf("id %q is a built-in model — choose a different id", c.ID)
		}
	}
	return nil
}

// AddCustom validates and persists a custom model to config (upsert by id).
func AddCustom(c config.CustomModel) error {
	if err := ValidateCustom(c); err != nil {
		return err
	}
	f, err := config.Load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.CustomModels {
		if f.CustomModels[i].ID == c.ID {
			f.CustomModels[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		f.CustomModels = append(f.CustomModels, c)
	}
	return config.Save(f)
}

// RemoveCustom drops a custom model from config. Returns false if not found.
func RemoveCustom(id string) (bool, error) {
	f, err := config.Load()
	if err != nil {
		return false, err
	}
	out := f.CustomModels[:0]
	found := false
	for _, c := range f.CustomModels {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return false, nil
	}
	f.CustomModels = out
	return true, config.Save(f)
}
