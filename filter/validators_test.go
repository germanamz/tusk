package filter

import (
	"testing"
)

func TestValidateStatus(test *testing.T) {
	valid := []string{"active", "pending", "pending,active", "pending,active,completed"}
	for _, value := range valid {
		if err := validateStatus(value); err != nil {
			test.Errorf("validateStatus(%q) unexpected error: %v", value, err)
		}
	}

	invalid := []string{"", ",", ",active", "active,"}
	for _, value := range invalid {
		if err := validateStatus(value); err == nil {
			test.Errorf("validateStatus(%q) expected error", value)
		}
	}
}

func TestValidateProject(test *testing.T) {
	if err := validateProject("backend"); err != nil {
		test.Errorf("validateProject(\"backend\") unexpected error: %v", err)
	}
	if err := validateProject(""); err == nil {
		test.Error("validateProject(\"\") expected error")
	}
}

func TestValidatePriority(test *testing.T) {
	valid := []string{"0", "1", "2", "3", "4", "none", "low", "medium", "high", "urgent", "2..4", "low..high", "4..4"}
	for _, value := range valid {
		if err := validatePriority(value); err != nil {
			test.Errorf("validatePriority(%q) unexpected error: %v", value, err)
		}
	}

	invalid := []string{"", "5", "-1", "critical", "abc", "5..6", "high..low"}
	for _, value := range invalid {
		if err := validatePriority(value); err == nil {
			test.Errorf("validatePriority(%q) expected error", value)
		}
	}
}

func TestValidateDue(test *testing.T) {
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
	for _, value := range valid {
		if err := validateDue(value); err != nil {
			test.Errorf("validateDue(%q) unexpected error: %v", value, err)
		}
	}

	invalid := []string{"notadate", "13-13-2026", "..friday", "today.."}
	for _, value := range invalid {
		if err := validateDue(value); err == nil {
			test.Errorf("validateDue(%q) expected error", value)
		}
	}
}

func TestValidateShortID(test *testing.T) {
	valid := []string{"", "a3f8b2c1", "DEADBEEF", "abcd1234", "abcdef012"}
	for _, value := range valid {
		if err := validateShortID(value); err != nil {
			test.Errorf("validateShortID(%q) unexpected error: %v", value, err)
		}
	}

	invalid := []string{"xyz!", "ab", "abc", "not-hex!!"}
	for _, value := range invalid {
		if err := validateShortID(value); err == nil {
			test.Errorf("validateShortID(%q) expected error", value)
		}
	}
}

func TestValidateBool(test *testing.T) {
	for _, value := range []string{"true", "false"} {
		if err := validateBool(value); err != nil {
			test.Errorf("validateBool(%q) unexpected error: %v", value, err)
		}
	}

	for _, value := range []string{"", "yes", "1", "TRUE"} {
		if err := validateBool(value); err == nil {
			test.Errorf("validateBool(%q) expected error", value)
		}
	}
}
