package domain

// UrgencyOverridesPatch describes an RFC 7396-style merge patch applied to
// a task's urgency_overrides column. ClearAll runs first, then Clear keys,
// then Set keys. See the spec's §1 for ordering rules.
type UrgencyOverridesPatch struct {
	Set      map[string]float64 // key → new value
	Clear    map[string]bool    // key → true means delete
	ClearAll bool               // drop every key
}
