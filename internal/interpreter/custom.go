package interpreter

import (
	"fmt"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/ref"
)

// ValidateCustom checks a user-cataloged interpreter before it is persisted.
func ValidateCustom(c config.CustomModel) error {
	if err := ref.ValidateSlug(c.ID); err != nil {
		return fmt.Errorf("id %q: %w", c.ID, err)
	}
	if c.Repo == "" {
		return fmt.Errorf("a HuggingFace repo is required (interpreters are pulled from the hub)")
	}
	for _, b := range builtin {
		if b.ID == c.ID {
			return fmt.Errorf("id %q is a built-in interpreter — choose a different id", c.ID)
		}
	}
	return nil
}

// AddCustom validates and persists a custom interpreter (upsert by id).
func AddCustom(c config.CustomModel) error {
	if err := ValidateCustom(c); err != nil {
		return err
	}
	f, err := config.Load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.CustomInterpreters {
		if f.CustomInterpreters[i].ID == c.ID {
			f.CustomInterpreters[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		f.CustomInterpreters = append(f.CustomInterpreters, c)
	}
	return config.Save(f)
}

// RemoveCustom drops a custom interpreter from config. Returns false if absent.
func RemoveCustom(id string) (bool, error) {
	f, err := config.Load()
	if err != nil {
		return false, err
	}
	out := f.CustomInterpreters[:0]
	found := false
	for _, c := range f.CustomInterpreters {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return false, nil
	}
	f.CustomInterpreters = out
	return true, config.Save(f)
}
