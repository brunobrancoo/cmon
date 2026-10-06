package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordEvent(t *testing.T) {
	sessDir = t.TempDir()
	t.Setenv("CLAUDE_PID", "4242")
	base := time.UnixMilli(1_000_000)
	read := func() session {
		var s session
		b, err := os.ReadFile(filepath.Join(sessDir, "s1.json"))
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(b, &s)
		return s
	}
	record := func(in hookInput, at time.Duration) {
		in.SessionID = "s1"
		if err := recordEvent(in, base.Add(at)); err != nil {
			t.Fatal(err)
		}
	}

	record(hookInput{HookEventName: "UserPromptSubmit", Cwd: "/p", TranscriptPath: "/t"}, 0)
	record(hookInput{HookEventName: "PostToolUse"}, time.Second)
	if s := read(); s.State != stateWorking || s.Since != base.UnixMilli() || s.PID != 4242 || s.Cwd != "/p" {
		t.Fatalf("working keeps its first since and the session info: %+v", s)
	}

	record(hookInput{HookEventName: "Notification", NotificationType: "permission_prompt"}, 2*time.Second)
	if s := read(); s.State != statePermission {
		t.Fatalf("want permission: %+v", s)
	}

	record(hookInput{HookEventName: "Notification", NotificationType: "idle_prompt"}, 3*time.Second)
	if s := read(); s.State != statePermission {
		t.Fatalf("idle notification changes nothing: %+v", s)
	}

	record(hookInput{HookEventName: "Stop"}, 5*time.Second)
	record(hookInput{HookEventName: "PostToolUse"}, 4*time.Second) // late async hook
	if s := read(); s.State != stateDone {
		t.Fatalf("an older event never overwrites a newer one: %+v", s)
	}

	record(hookInput{HookEventName: "SessionEnd"}, 6*time.Second)
	if _, err := os.Stat(filepath.Join(sessDir, "s1.json")); !os.IsNotExist(err) {
		t.Fatal("SessionEnd removes the session")
	}
}

func TestSoundFor(t *testing.T) {
	cases := []struct {
		name string
		prev map[string]string
		now  []view
		want string
	}{
		{"needs permission", map[string]string{"a": stateWorking}, []view{{ID: "a", State: statePermission}}, soundAttention},
		{"one done, other busy", map[string]string{"a": stateWorking, "b": stateWorking},
			[]view{{ID: "a", State: stateDone}, {ID: "b", State: stateWorking}}, soundDone},
		{"last one done", map[string]string{"a": stateWorking, "b": stateDone},
			[]view{{ID: "a", State: stateDone}, {ID: "b", State: stateDone}}, soundAllDone},
		{"nothing changed", map[string]string{"a": stateDone}, []view{{ID: "a", State: stateDone}}, ""},
		{"new session working", map[string]string{}, []view{{ID: "a", State: stateWorking}}, ""},
	}
	for _, c := range cases {
		if got := soundFor(c.prev, c.now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
