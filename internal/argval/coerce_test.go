package argval_test

import (
	"testing"

	"github.com/germanamz/tusk/internal/argval"
)

func TestString(test *testing.T) {
	val, err := argval.String(map[string]any{"k": "v"}, "k")

	if err != nil || val != "v" {
		test.Fatalf("present: got (%q, %v), want (\"v\", nil)", val, err)
	}

	val, err = argval.String(map[string]any{}, "k")

	if err != nil || val != "" {
		test.Fatalf("absent: got (%q, %v), want (\"\", nil)", val, err)
	}

	_, err = argval.String(map[string]any{"k": 3}, "k")

	if err == nil || err.Error() != `arg "k" has type int, want string` {
		test.Fatalf("wrong type: got %v", err)
	}
}

func TestInt(test *testing.T) {
	cases := []struct {
		raw  any
		want int
	}{
		{int(5), 5},
		{int64(7), 7},
		{float64(3.0), 3},
	}

	for _, testCase := range cases {
		got, err := argval.Int(map[string]any{"k": testCase.raw}, "k")

		if err != nil || got != testCase.want {
			test.Errorf("Int(%T %v): got (%d, %v), want (%d, nil)", testCase.raw, testCase.raw, got, err, testCase.want)
		}
	}

	if got, err := argval.Int(map[string]any{}, "k"); err != nil || got != 0 {
		test.Errorf("absent: got (%d, %v), want (0, nil)", got, err)
	}

	_, err := argval.Int(map[string]any{"k": 2.5}, "k")

	if err == nil || err.Error() != `arg "k" has type float64 (non-integer float), want int` {
		test.Errorf("non-integer float: got %v", err)
	}

	_, err = argval.Int(map[string]any{"k": "x"}, "k")

	if err == nil || err.Error() != `arg "k" has type string, want int` {
		test.Errorf("wrong type: got %v", err)
	}
}

func TestFloat(test *testing.T) {
	for _, raw := range []any{float64(4), int(4), int64(4)} {
		got, err := argval.Float(map[string]any{"k": raw}, "k")

		if err != nil || got != 4 {
			test.Errorf("Float(%T): got (%v, %v), want (4, nil)", raw, got, err)
		}
	}

	_, err := argval.Float(map[string]any{"k": "x"}, "k")

	if err == nil || err.Error() != `arg "k" has type string, want float64` {
		test.Errorf("wrong type: got %v", err)
	}
}

func TestBool(test *testing.T) {
	got, err := argval.Bool(map[string]any{"k": true}, "k")

	if err != nil || !got {
		test.Fatalf("present: got (%v, %v), want (true, nil)", got, err)
	}

	_, err = argval.Bool(map[string]any{"k": 1}, "k")

	if err == nil || err.Error() != `arg "k" has type int, want bool` {
		test.Errorf("wrong type: got %v", err)
	}
}

func TestStringSlice(test *testing.T) {
	got, err := argval.StringSlice(map[string]any{"k": "solo"}, "k")

	if err != nil || len(got) != 1 || got[0] != "solo" {
		test.Fatalf("bare string: got (%v, %v), want ([solo], nil)", got, err)
	}

	got, err = argval.StringSlice(map[string]any{"k": []any{"a", "b"}}, "k")

	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		test.Fatalf("slice: got (%v, %v), want ([a b], nil)", got, err)
	}

	_, err = argval.StringSlice(map[string]any{"k": 7}, "k")

	if err == nil || err.Error() != `arg "k" has type int, want []string` {
		test.Errorf("wrong type: got %v", err)
	}

	_, err = argval.StringSlice(map[string]any{"k": []any{"a", 7}}, "k")

	if err == nil || err.Error() != `arg "k" element 1 has type int, want string` {
		test.Errorf("bad element: got %v", err)
	}
}
