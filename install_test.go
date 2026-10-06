package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const userSettings = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "[ -x a ] && a || true"
          }
        ]
      }
    ]
  },
  "theme": "dark"
}
`

func TestInstallThenUninstallRestoresSettings(t *testing.T) {
	settingsPath = filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(settingsPath, []byte(userSettings), 0o644)

	for range 2 { // installing twice registers the hooks once
		if err := editHooks("/opt/bin/cmon", true); err != nil {
			t.Fatal(err)
		}
	}
	installed, _ := os.ReadFile(settingsPath)
	if n := strings.Count(string(installed), `"/opt/bin/cmon"`); n != len(hookEvents) {
		t.Fatalf("want %d cmon hooks, got %d:\n%s", len(hookEvents), n, installed)
	}
	if !strings.Contains(string(installed), `"matcher": "AskUserQuestion"`) {
		t.Fatalf("PreToolUse matcher missing:\n%s", installed)
	}

	if err := editHooks("/opt/bin/cmon", false); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(settingsPath)
	if string(restored) != userSettings {
		t.Fatalf("settings changed:\n%s", restored)
	}
}
