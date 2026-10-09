package index

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"sync"

	"modernc.org/sqlite"
)

// SQLite reserves the `X REGEXP Y` operator but ships no implementation: it
// calls a user function named regexp with the pattern first, `regexp(Y, X)`.
// Registering it here, at package init, makes it available on every
// connection the driver opens, before any Open runs. The filter compiler
// emits `path REGEXP ?` for glob patterns on path and id (see
// internal/pathglob), the one engine-specific spelling of "matches a regex".
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("regexp", 2, regexpFunction)
}

// regexpCacheLimit bounds the compiled-pattern cache. SQLite calls regexp once
// per row with the same pattern, so a handful of entries covers every query in
// flight; the bound only keeps a long-running daemon from accumulating every
// pattern it has ever seen.
const regexpCacheLimit = 64

var regexpCache = struct {
	sync.Mutex
	compiled map[string]*regexp.Regexp
}{compiled: map[string]*regexp.Regexp{}}

// regexpFunction implements regexp(pattern, value). A NULL on either side
// yields NULL, matching SQLite's built-in operators; a pattern that does not
// compile is an SQL error rather than a silent non-match.
func regexpFunction(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	pattern, patternOK := textArg(args[0])
	value, valueOK := textArg(args[1])

	if !patternOK || !valueOK {
		return nil, nil
	}

	compiled, compileErr := cachedRegexp(pattern)

	if compileErr != nil {
		return nil, compileErr
	}

	if compiled.MatchString(value) {
		return int64(1), nil
	}

	return int64(0), nil
}

// textArg reads a TEXT or BLOB argument as a string; NULL and other types
// report false.
func textArg(arg driver.Value) (string, bool) {
	switch typed := arg.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	}

	return "", false
}

func cachedRegexp(pattern string) (*regexp.Regexp, error) {
	regexpCache.Lock()
	defer regexpCache.Unlock()

	if compiled, hit := regexpCache.compiled[pattern]; hit {
		return compiled, nil
	}

	compiled, compileErr := regexp.Compile(pattern)

	if compileErr != nil {
		return nil, fmt.Errorf("regexp: %w", compileErr)
	}

	if len(regexpCache.compiled) >= regexpCacheLimit {
		clear(regexpCache.compiled)
	}

	regexpCache.compiled[pattern] = compiled

	return compiled, nil
}
