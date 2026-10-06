package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// limit is one usage window, as shown by Claude Code's /usage.
type limit struct {
	Label    string  `json:"label"`
	Percent  float64 `json:"percent"`
	ResetsAt int64   `json:"resetsAt"` // unix ms, 0 when unknown
}

// fetchUsage asks the endpoint behind /usage, authenticated with the OAuth
// token Claude Code already stores. It only reads the token, never refreshes it.
func fetchUsage(client *http.Client) ([]limit, int, error) {
	token, err := oauthToken()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("usage: HTTP %d", resp.StatusCode)
	}
	type window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	var body struct {
		FiveHour *window `json:"five_hour"`
		SevenDay *window `json:"seven_day"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, err
	}
	var limits []limit
	for _, w := range []struct {
		label string
		w     *window
	}{{"5h", body.FiveHour}, {"7d", body.SevenDay}} {
		if w.w == nil || w.w.Utilization == nil {
			continue
		}
		l := limit{Label: w.label, Percent: *w.w.Utilization}
		if t, err := time.Parse(time.RFC3339, w.w.ResetsAt); err == nil {
			l.ResetsAt = t.UnixMilli()
		}
		limits = append(limits, l)
	}
	return limits, resp.StatusCode, nil
}

// oauthToken reads Claude Code's login: the macOS Keychain item
// "Claude Code-credentials", or ~/.claude/.credentials.json when present.
func oauthToken() (string, error) {
	data, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		data, err = exec.Command("/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		if err != nil {
			return "", fmt.Errorf("claude login not found: %w", err)
		}
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil || creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("claude login has no oauth token")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}
