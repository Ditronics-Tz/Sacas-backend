package controllers

import (
	"testing"

	"go_boilerplate/internal/services"
)

func TestValidPassword(t *testing.T) {
	cases := []struct {
		pw   string
		want bool
	}{
		{"Password1", true},
		{"Str0ngPass", true},
		{"short1A", false},       // < 8 chars
		{"alllowercase1", false}, // no uppercase
		{"ALLUPPERCASE1", false}, // no lowercase
		{"NoDigitsHere", false},  // no digit
		{"password", false},      // classic weak password
		{"Password1 with spaces", true},
	}
	for _, tc := range cases {
		if got := validPassword(tc.pw); got != tc.want {
			t.Errorf("validPassword(%q) = %v, want %v", tc.pw, got, tc.want)
		}
	}
}

func TestOTPAttemptKeyLayout(t *testing.T) {
	got := services.OTPAttemptKey("verify", "a@b.com")
	want := "otp_attempts:verify:a@b.com"
	if got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}
