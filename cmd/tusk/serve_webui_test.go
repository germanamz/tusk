package main

import (
	"slices"
	"testing"
)

func TestDeriveAllowedHosts(test *testing.T) {
	cases := []struct {
		addr string
		want []string
	}{
		{"127.0.0.1:7373", nil},
		{"localhost:7373", nil},
		{"[::1]:7373", nil},
		{":7373", []string{"*"}},
		{"0.0.0.0:7373", []string{"*"}},
		{"192.168.1.5:7373", []string{"192.168.1.5"}},
	}

	for _, testCase := range cases {
		if got := deriveAllowedHosts(testCase.addr); !slices.Equal(got, testCase.want) {
			test.Errorf("deriveAllowedHosts(%q) = %v, want %v", testCase.addr, got, testCase.want)
		}
	}
}

func TestIsLoopbackAddr(test *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7373": true,
		"localhost:7373": true,
		"[::1]:7373":     true,
		"0.0.0.0:7373":   false,
		"192.168.1.5:80": false,
		":7373":          false, // all-interfaces
	}

	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			test.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
