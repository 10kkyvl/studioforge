package config

import "testing"

func TestLoopbackValidation(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		if !IsLoopbackHost(host) {
			t.Errorf("%s rejected", host)
		}
	}
	opts := Options{Host: "0.0.0.0"}
	if err := opts.Normalize(); err == nil {
		t.Fatal("unsafe host accepted")
	}
	opts.UnsafeHost = true
	if err := opts.Normalize(); err != nil {
		t.Fatal(err)
	}
}
