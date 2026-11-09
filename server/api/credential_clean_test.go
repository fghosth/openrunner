package api

import "testing"

func TestCleanCredentialValue(t *testing.T) {
	cases := map[string]string{
		"example-token-0001":                      "example-token-0001",
		"  oauth1_token = example-token-0001  ":   "example-token-0001",
		`oauth1_secret = "fake-secret-for-tests"`: "fake-secret-for-tests",
		"Bearer eyJhbGciOiJ":                      "eyJhbGciOiJ", // JWT 头片段，不是凭据
		"oauth1_token=example-token-0001\n":       "example-token-0001",
	}
	for in, want := range cases {
		if got := cleanCredentialValue(in); got != want {
			t.Errorf("cleanCredentialValue(%q) = %q, 期望 %q", in, got, want)
		}
	}
}
