package main

import "testing"

func TestValidateBindAddress(t *testing.T) {
	cases := []struct {
		addr string
		flag bool
		env  string
		ok   bool
	}{
		{"127.0.0.1:8080", false, "", true},
		{"[::1]:8080", false, "production", true},
		{"0.0.0.0:8080", false, "development", false},
		{"0.0.0.0:8080", true, "production", false},
		{"0.0.0.0:8080", true, "development", true},
		{"example.com:8080", true, "development", false},
		{"invalid", true, "development", false},
	}
	for _, c := range cases {
		if err := validateBindAddress(c.addr, c.flag, c.env); (err == nil) != c.ok {
			t.Errorf("validateBindAddress(%q, %v, %q) = %v", c.addr, c.flag, c.env, err)
		}
	}
}
