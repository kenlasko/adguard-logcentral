package config

import (
	"strings"
	"testing"
)

// mapGetenv returns a getenv function backed by a map, so tests are hermetic
// and do not depend on process environment state.
func mapGetenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadInstancesHappyPath(t *testing.T) {
	env := map[string]string{
		"ADGUARD_1_URL":      "http://10.0.0.2:3000",
		"ADGUARD_1_USERNAME": "admin",
		"ADGUARD_1_PASSWORD": "secret1",
		"ADGUARD_1_NAME":     "dns1",
		"ADGUARD_2_URL":      "http://10.0.0.3:3000/",
		"ADGUARD_2_USERNAME": "admin",
		"ADGUARD_2_PASSWORD": "secret2",
	}

	instances, errs := loadInstances(mapGetenv(env))
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
	if instances[0].Name != "dns1" {
		t.Errorf("instance 1 name = %q, want dns1", instances[0].Name)
	}
	// Name defaults to URL host when not provided.
	if instances[1].Name != "10.0.0.3:3000" {
		t.Errorf("instance 2 name = %q, want host default", instances[1].Name)
	}
	// Trailing slash stripped.
	if instances[1].URL != "http://10.0.0.3:3000" {
		t.Errorf("instance 2 url = %q, want trailing slash stripped", instances[1].URL)
	}
}

func TestLoadInstancesMissingCredentials(t *testing.T) {
	env := map[string]string{
		"ADGUARD_1_URL": "http://10.0.0.2:3000",
	}
	_, errs := loadInstances(mapGetenv(env))
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors (username+password), got %d: %v", len(errs), errs)
	}
	joined := joinErrs(errs)
	if !strings.Contains(joined, "USERNAME") || !strings.Contains(joined, "PASSWORD") {
		t.Errorf("errors should mention both missing credentials: %s", joined)
	}
}

func TestLoadInstancesIndexGap(t *testing.T) {
	env := map[string]string{
		"ADGUARD_1_URL":      "http://a:3000",
		"ADGUARD_1_USERNAME": "u",
		"ADGUARD_1_PASSWORD": "p",
		// index 2 missing entirely
		"ADGUARD_3_URL":      "http://c:3000",
		"ADGUARD_3_USERNAME": "u",
		"ADGUARD_3_PASSWORD": "p",
	}
	_, errs := loadInstances(mapGetenv(env))
	if len(errs) == 0 {
		t.Fatal("expected a gap error, got none")
	}
	if !strings.Contains(joinErrs(errs), "gap") {
		t.Errorf("expected gap error, got %s", joinErrs(errs))
	}
}

func TestLoadInstancesGapWithPartialData(t *testing.T) {
	// Index 2 has a username but no URL: a typo we want to catch.
	env := map[string]string{
		"ADGUARD_1_URL":      "http://a:3000",
		"ADGUARD_1_USERNAME": "u",
		"ADGUARD_1_PASSWORD": "p",
		"ADGUARD_2_USERNAME": "orphan",
	}
	_, errs := loadInstances(mapGetenv(env))
	if !strings.Contains(joinErrs(errs), "index gap") {
		t.Errorf("expected index gap error for orphan data, got %s", joinErrs(errs))
	}
}

func TestLoadInstancesDuplicateNames(t *testing.T) {
	env := map[string]string{
		"ADGUARD_1_URL":      "http://a:3000",
		"ADGUARD_1_USERNAME": "u",
		"ADGUARD_1_PASSWORD": "p",
		"ADGUARD_1_NAME":     "same",
		"ADGUARD_2_URL":      "http://b:3000",
		"ADGUARD_2_USERNAME": "u",
		"ADGUARD_2_PASSWORD": "p",
		"ADGUARD_2_NAME":     "same",
	}
	_, errs := loadInstances(mapGetenv(env))
	if !strings.Contains(joinErrs(errs), "duplicate instance name") {
		t.Errorf("expected duplicate name error, got %s", joinErrs(errs))
	}
}

func TestLoadInstancesInvalidURL(t *testing.T) {
	env := map[string]string{
		"ADGUARD_1_URL":      "not-a-url",
		"ADGUARD_1_USERNAME": "u",
		"ADGUARD_1_PASSWORD": "p",
	}
	_, errs := loadInstances(mapGetenv(env))
	if !strings.Contains(joinErrs(errs), "not a valid absolute URL") {
		t.Errorf("expected invalid URL error, got %s", joinErrs(errs))
	}
}

func TestLoadInstancesNoneConfigured(t *testing.T) {
	_, errs := loadInstances(mapGetenv(map[string]string{}))
	if !strings.Contains(joinErrs(errs), "no AdGuard instances") {
		t.Errorf("expected no-instances error, got %s", joinErrs(errs))
	}
}

func joinErrs(errs []error) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}
