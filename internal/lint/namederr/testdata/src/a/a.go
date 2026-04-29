// Package a contains test fixtures for the namederr analyzer.
package a

import "errors"

// singleErr — passes: only one err := in the function body, no flag.
func singleErr() error {
	_, err := doSomething()
	if err != nil {
		return err
	}

	return nil
}

// differentBlocksErr — passes: each branch has exactly one err :=, no flag.
func differentBlocksErr(cond bool) error {
	if cond {
		_, err := doSomething()
		if err != nil {
			return err
		}
	} else {
		_, err := doSomethingElse()
		if err != nil {
			return err
		}
	}

	return nil
}

// twoShadow — fails: two err := siblings in the same block; both are flagged.
func twoShadow() error {
	aVal, err := doSomething() // want "namederr: 'err' is shadowed 2 times in this scope; rename all instances to typed names \\(e\\.g\\. fooErr, barErr\\)"
	if err != nil {
		return err
	}

	bVal, err := doSomethingElse() // want "namederr: 'err' is shadowed 2 times in this scope; rename all instances to typed names \\(e\\.g\\. fooErr, barErr\\)"
	if err != nil {
		return err
	}

	_, _ = aVal, bVal
	return nil
}

// threeShadow — fails: three err := siblings in the same block; all three flagged.
func threeShadow() error {
	aResult, err := doSomething() // want "namederr: 'err' is shadowed 3 times in this scope; rename all instances to typed names \\(e\\.g\\. fooErr, barErr\\)"
	if err != nil {
		return err
	}

	bResult, err := doSomethingElse() // want "namederr: 'err' is shadowed 3 times in this scope; rename all instances to typed names \\(e\\.g\\. fooErr, barErr\\)"
	if err != nil {
		return err
	}

	cResult, err := produceInt() // want "namederr: 'err' is shadowed 3 times in this scope; rename all instances to typed names \\(e\\.g\\. fooErr, barErr\\)"
	if err != nil {
		return err
	}

	_, _, _ = aResult, bResult, cResult
	return nil
}

// nestedFuncLiteralIsolated — passes: outer block has one err :=, the closure
// also has its own one err := in its own block. Each block has count=1, no flag.
func nestedFuncLiteralIsolated() error {
	_, err := doSomething()
	if err != nil {
		return err
	}

	fn := func() error {
		_, err := doSomethingElse()
		if err != nil {
			return err
		}

		return nil
	}

	return fn()
}

// nonDefineDoesNotCount — passes: var + = assignment does not use :=;
// the analyzer must not fire even if err is assigned multiple times with =.
func nonDefineDoesNotCount() error {
	var err error

	err = errSentinel
	if err != nil {
		return err
	}

	err = nil
	_ = err

	return nil
}

// ---- stubs to keep package syntax valid ----

var errSentinel = errors.New("sentinel")

func doSomething() (int, error)     { return 0, errSentinel }
func doSomethingElse() (int, error) { return 0, errSentinel }
func produceInt() (int, error)      { return 42, nil }
