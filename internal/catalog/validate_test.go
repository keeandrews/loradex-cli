package catalog

import "testing"

func valid() *Catalog {
	return &Catalog{
		Name: "flux2-klein-portrait", Description: "ok", Visibility: "public",
		BaseModel: "flux2-klein", Format: "safetensors", License: "MIT",
		Weights: "model.safetensors", RecommendedWeight: 0.8,
		Tags: []string{"portrait"}, TriggerWords: []string{"ohwxman"},
	}
}

func TestValidate_Good(t *testing.T) {
	r := Validate(valid())
	if !r.OK() {
		t.Fatalf("valid catalog failed: %v", r.Errors)
	}
}

func TestValidate_CollectsAll(t *testing.T) {
	c := valid()
	c.Name = "BAD"
	c.Visibility = "secret"
	c.RecommendedWeight = 9
	c.Weights = "/etc/passwd"
	r := Validate(c)
	if r.OK() {
		t.Fatal("expected errors")
	}
	if len(r.Errors) < 4 {
		t.Errorf("expected >=4 errors, got %d: %v", len(r.Errors), r.Errors)
	}
}

func TestValidate_VisibilityStrict(t *testing.T) {
	c := valid()
	c.Visibility = "Public" // case-sensitive strict
	if Validate(c).OK() {
		t.Error("visibility should be strict")
	}
}

func TestValidate_UnknownBaseWarnsNotFails(t *testing.T) {
	c := valid()
	c.BaseModel = "brand-new-model"
	c.Format = "exotic"
	r := Validate(c)
	if !r.OK() {
		t.Errorf("unknown base/format should warn, not fail: %v", r.Errors)
	}
	if len(r.Warnings) < 2 {
		t.Errorf("expected 2 warnings, got %v", r.Warnings)
	}
}

func TestValidate_Caps(t *testing.T) {
	c := valid()
	long := make([]byte, MaxDescription+1)
	for i := range long {
		long[i] = 'a'
	}
	c.Description = string(long)
	if Validate(c).OK() {
		t.Error("over-long description should fail")
	}
}
