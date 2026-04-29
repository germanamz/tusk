// Package fixtures contains test fixtures for the testhandle analyzer.
package fixtures

import "testing"

// --- Passing cases ---

// TestX passes: *testing.T parameter named "test".
func TestX(test *testing.T) {}

// BenchmarkX passes: *testing.B parameter named "bench".
func BenchmarkX(bench *testing.B) {}

// helperX passes: testing.TB parameter named "harness".
func helperX(harness testing.TB) {}

// blankT passes: blank identifier exemption for *testing.T.
func blankT(_ *testing.T) {}

// blankB passes: blank identifier exemption for *testing.B.
func blankB(_ *testing.B) {}

// blankTB passes: blank identifier exemption for testing.TB.
func blankTB(_ testing.TB) {}

// nonTestParam passes: non-testing parameter types are not flagged.
func nonTestParam(xVal int, name string) { _, _ = xVal, name }

// --- Failing cases ---

// badT fails: *testing.T parameter named "t" instead of "test".
func badT(t *testing.T) { // want "testhandle: parameter of type \\*testing\\.T must be named \"test\", got \"t\""
	_ = t
}

// badB fails: *testing.B parameter named "b" instead of "bench".
func badB(b *testing.B) { // want "testhandle: parameter of type \\*testing\\.B must be named \"bench\", got \"b\""
	_ = b
}

// badTB fails: testing.TB parameter named "tb" instead of "harness".
func badTB(tb testing.TB) { // want "testhandle: parameter of type testing\\.TB must be named \"harness\", got \"tb\""
	_ = tb
}

// --- Multi-name field: both names checked independently ---

// multiName: one correct name, one incorrect name in the same field.
// "test" is correct, "wrongT" is not — only wrongT is flagged.
func multiName(test, wrongT *testing.T) { // want "testhandle: parameter of type \\*testing\\.T must be named \"test\", got \"wrongT\""
	_, _ = test, wrongT
}

// --- Function literal (*ast.FuncType) coverage ---

// funcLiteralCoverage verifies that FuncType nodes inside function literals
// are also checked. The closure uses wrong name "myTest" instead of "test".
var funcLiteralCoverage = func(myTest *testing.T) { // want "testhandle: parameter of type \\*testing\\.T must be named \"test\", got \"myTest\""
	_ = myTest
}
