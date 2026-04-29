// Package a contains test fixtures for the blankline analyzer.
package a

import "errors"

// goodSpacing demonstrates the happy path: blank lines both before and after.
func goodSpacing() error {
	val, err := doSomething()

	if err != nil {
		return err
	}

	_ = val
	return nil
}

// missingBefore has no blank line between the assignment and the if-guard.
func missingBefore() error {
	val, err := doSomething() // want "blankline: missing blank line before if-err guard"
	if err != nil {
		return err
	}

	_ = val
	return nil
}

// missingAfter has no blank line between the if-guard's closing brace and the
// next statement.
func missingAfter() error {
	val, err := doSomething()

	if err != nil { // want "blankline: missing blank line after if-err guard"
		return err
	}
	_ = val
	return nil
}

// lastInBlock — the if-guard is the last statement; the after-case must NOT fire.
func lastInBlock() error {
	_, err := doSomething()

	if err != nil {
		return err
	}

	return nil
}

// singleErrProperly demonstrates a single assignment with correct spacing.
func singleErrProperly() (int, error) {
	n, err := produceInt()

	if err != nil {
		return 0, err
	}

	return n, nil
}

// namedErrBefore uses the <noun>Err pattern and is missing the blank before.
func namedErrBefore() error {
	_, fooErr := doSomething() // want "blankline: missing blank line before if-err guard"
	if fooErr != nil {
		return fooErr
	}

	return nil
}

// namedErrAfter uses the <noun>Err pattern and is missing the blank after.
func namedErrAfter() error {
	_, fooErr := doSomething()

	if fooErr != nil { // want "blankline: missing blank line after if-err guard"
		return fooErr
	}
	return nil
}

// namedErrGood uses the <noun>Err pattern with correct spacing.
func namedErrGood() error {
	_, fooErr := doSomething()

	if fooErr != nil {
		return fooErr
	}

	return nil
}

// ---- stubs to keep package syntax valid ----

var errSentinel = errors.New("sentinel")

func doSomething() (int, error) { return 0, errSentinel }
func produceInt() (int, error)  { return 42, nil }
