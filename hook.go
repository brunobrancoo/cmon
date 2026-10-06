package main

import (
	"cmp"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type hookInput struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	Cwd              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
}

// stateFor maps a hook event to the session state it implies.
// An empty state means the event carries no state change.
func stateFor(in hookInput) string {
	switch in.HookEventName {
	case "UserPromptSubmit", "PostToolUse":
		return stateWorking
	case "PreToolUse": // registered only for AskUserQuestion
		return stateQuestion
	case "Stop":
		return stateDone
	case "Notification":
		switch in.NotificationType {
		case "permission_prompt":
			return statePermission
		case "elicitation_dialog", "elicitation_url_dialog":
			return stateQuestion
		}
	}
	return ""
}

func runHook() {
	var in hookInput
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || json.Unmarshal(data, &in) != nil || in.SessionID == "" {
		return
	}
	_ = recordEvent(in, time.Now())
}

func recordEvent(in hookInput, now time.Time) error {
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return err
	}
	// async hooks of the same session can run concurrently
	lock, err := os.OpenFile(filepath.Join(sessDir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}

	file := filepath.Join(sessDir, in.SessionID+".json")
	if in.HookEventName == "SessionEnd" {
		return os.Remove(file)
	}
	state := stateFor(in)
	if state == "" {
		return nil
	}

	var prev session
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &prev)
	}
	ts := now.UnixMilli()
	if ts < prev.Updated { // an older event that lost the race to a newer one
		return nil
	}
	since := ts
	// a repeated permission prompt is a new request, so it restarts the clock
	if prev.State == state && state != statePermission {
		since = prev.Since
	}
	pid := prev.PID
	if pid == 0 {
		pid = claudePID()
	}
	s := session{
		State:      state,
		Since:      since,
		Updated:    ts,
		PID:        pid,
		Cwd:        cmp.Or(in.Cwd, prev.Cwd),
		Transcript: cmp.Or(in.TranscriptPath, prev.Transcript),
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// claudePID is the Claude Code process. Hooks run in exec form (no shell),
// so the parent process is Claude Code itself.
func claudePID() int {
	if pid, err := strconv.Atoi(os.Getenv("CLAUDE_PID")); err == nil && pid > 0 {
		return pid
	}
	return os.Getppid()
}
