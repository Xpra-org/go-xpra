package client

import (
	"errors"
	"strings"
	"testing"
)

func TestChallengePasswordPrecedence(t *testing.T) {
	t.Run("URL", func(t *testing.T) {
		t.Setenv("XPRA_PASSWORD", "environment-password")
		prompted := false
		client := &Client{
			password: "url-password",
			passwordPrompt: func(string) (string, error) {
				prompted = true
				return "prompt-password", nil
			},
		}
		password, err := client.challengePassword("Password")
		if err != nil {
			t.Fatalf("challengePassword: %v", err)
		}
		if password != "url-password" {
			t.Errorf("password = %q, want URL password", password)
		}
		if prompted {
			t.Error("interactive prompt was called")
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("XPRA_PASSWORD", "environment-password")
		prompted := false
		client := &Client{
			passwordPrompt: func(string) (string, error) {
				prompted = true
				return "prompt-password", nil
			},
		}
		password, err := client.challengePassword("Password")
		if err != nil {
			t.Fatalf("challengePassword: %v", err)
		}
		if password != "environment-password" {
			t.Errorf("password = %q, want environment password", password)
		}
		if prompted {
			t.Error("interactive prompt was called")
		}
	})

	t.Run("prompt", func(t *testing.T) {
		t.Setenv("XPRA_PASSWORD", "")
		var description string
		client := &Client{
			passwordPrompt: func(got string) (string, error) {
				description = got
				return "prompt-password", nil
			},
		}
		password, err := client.challengePassword("Server password")
		if err != nil {
			t.Fatalf("challengePassword: %v", err)
		}
		if password != "prompt-password" {
			t.Errorf("password = %q, want prompted password", password)
		}
		if description != "Server password" {
			t.Errorf("prompt description = %q, want server description", description)
		}
	})
}

func TestChallengePasswordPromptErrors(t *testing.T) {
	t.Setenv("XPRA_PASSWORD", "")

	t.Run("not configured", func(t *testing.T) {
		_, err := (&Client{}).challengePassword("Password")
		if err == nil || !strings.Contains(err.Error(), "XPRA_PASSWORD") {
			t.Errorf("error = %v, want non-interactive credential advice", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		client := &Client{passwordPrompt: func(string) (string, error) {
			return "", errors.New("cancelled")
		}}
		_, err := client.challengePassword("Password")
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("error = %v, want cancellation", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		client := &Client{passwordPrompt: func(string) (string, error) {
			return "", nil
		}}
		_, err := client.challengePassword("Password")
		if err == nil || !strings.Contains(err.Error(), "no password") {
			t.Errorf("error = %v, want empty-password error", err)
		}
	})
}
