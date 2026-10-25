package api

import "testing"

func TestCleanCredentialValue(t *testing.T) {
	cases := map[string]string{
		"REDACTED-OAUTH-TOKEN-c417":                           "REDACTED-OAUTH-TOKEN-c417",
		"  oauth1_token = REDACTED-OAUTH-TOKEN-c417  ":        "REDACTED-OAUTH-TOKEN-c417",
		`oauth1_secret = "REDACTED-OAUTH-SECRET"`: "REDACTED-OAUTH-SECRET",
		"Bearer eyJhbGciOiJ":                      "eyJhbGciOiJ",
		"oauth1_token=REDACTED-OAUTH-TOKEN\n":                 "REDACTED-OAUTH-TOKEN",
	}
	for in, want := range cases {
		if got := cleanCredentialValue(in); got != want {
			t.Errorf("cleanCredentialValue(%q) = %q, 期望 %q", in, got, want)
		}
	}
}
