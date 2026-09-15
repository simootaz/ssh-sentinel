package model

import (
	"strings"
	"testing"
)

func TestValidCountry(t *testing.T) {
	for _, code := range []string{"FR", "KP", "US"} {
		if !ValidCountry(code) {
			t.Errorf("ValidCountry(%q) = false, want true", code)
		}
	}
	// Unassigned, too short, alpha-3, empty: the handler upper-cases and
	// trims before asking, so only the shape and the assignment are checked here.
	for _, code := range []string{"XX", "F", "fra", ""} {
		if ValidCountry(code) {
			t.Errorf("ValidCountry(%q) = true, want false", code)
		}
	}
}

func TestValidCountryListComplete(t *testing.T) {
	// ISO 3166-1 has 249 officially assigned alpha-2 codes.
	if got := len(isoCountries); got != 249 {
		t.Fatalf("%d codes, want 249", got)
	}
	for code := range isoCountries {
		if len(code) != 2 || code != strings.ToUpper(code) {
			t.Errorf("code %q is not two upper-case letters", code)
		}
	}
}
