package protocol

import "testing"

func TestEnvBool(t *testing.T) {
	tests := []struct {
		value    string
		fallback bool
		want     bool
	}{
		{"", true, true},
		{"", false, false},
		{"yes", false, true},
		{"TRUE", false, true},
		{"on", false, true},
		{"1", false, true},
		{"2", false, true},
		{"no", true, false},
		{"FALSE", true, false},
		{"off", true, false},
		{"0", true, false},
		{"invalid", true, true},
		{"invalid", false, false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("GO_XPRA_TEST_BOOL", test.value)
			if got := envBool("GO_XPRA_TEST_BOOL", test.fallback); got != test.want {
				t.Errorf("envBool(%q, %v) = %v, want %v",
					test.value, test.fallback, got, test.want)
			}
		})
	}
}
