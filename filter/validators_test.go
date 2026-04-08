package filter

import (
	"testing"
)

func TestValidateStatus(t *testing.T) {
	valid := []string{"active", "pending", "pending,active", "pending,active,completed"}
	for _, v := range valid {
		if err := validateStatus(v); err != nil {
			t.Errorf("validateStatus(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", ",", ",active", "active,"}
	for _, v := range invalid {
		if err := validateStatus(v); err == nil {
			t.Errorf("validateStatus(%q) expected error", v)
		}
	}
}

func TestValidateProject(t *testing.T) {
	if err := validateProject("backend"); err != nil {
		t.Errorf("validateProject(\"backend\") unexpected error: %v", err)
	}
	if err := validateProject(""); err == nil {
		t.Error("validateProject(\"\") expected error")
	}
}

func TestValidatePriority(t *testing.T) {
	valid := []string{"0", "1", "2", "3", "4", "none", "low", "medium", "high", "urgent", "2..4", "low..high", "4..4"}
	for _, v := range valid {
		if err := validatePriority(v); err != nil {
			t.Errorf("validatePriority(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "5", "-1", "critical", "abc", "5..6", "high..low"}
	for _, v := range invalid {
		if err := validatePriority(v); err == nil {
			t.Errorf("validatePriority(%q) expected error", v)
		}
	}
}

func TestValidateDue(t *testing.T) {
	valid := []string{
		"",
		"2026-04-10",
		"2026-04-10T15:30:00Z",
		"2026-04-10T15:30:00+02:00",
		"today",
		"tomorrow",
		"thisweek",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		"today..friday",
		"2026-04-01..2026-04-10",
	}
	for _, v := range valid {
		if err := validateDue(v); err != nil {
			t.Errorf("validateDue(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"notadate", "13-13-2026", "..friday", "today.."}
	for _, v := range invalid {
		if err := validateDue(v); err == nil {
			t.Errorf("validateDue(%q) expected error", v)
		}
	}
}

func TestValidateShortID(t *testing.T) {
	valid := []string{"", "a3f8b2c1", "DEADBEEF", "abcd1234", "abcdef012"}
	for _, v := range valid {
		if err := validateShortID(v); err != nil {
			t.Errorf("validateShortID(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"xyz!", "ab", "abc", "not-hex!!"}
	for _, v := range invalid {
		if err := validateShortID(v); err == nil {
			t.Errorf("validateShortID(%q) expected error", v)
		}
	}
}

func TestValidateBool(t *testing.T) {
	for _, v := range []string{"true", "false"} {
		if err := validateBool(v); err != nil {
			t.Errorf("validateBool(%q) unexpected error: %v", v, err)
		}
	}

	for _, v := range []string{"", "yes", "1", "TRUE"} {
		if err := validateBool(v); err == nil {
			t.Errorf("validateBool(%q) expected error", v)
		}
	}
}
