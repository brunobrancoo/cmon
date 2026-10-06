package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const agentLabel = "local.cmon"

var (
	settingsPath = filepath.Join(home, ".claude", "settings.json")
	agentPath    = filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
)

// hookEvents are the Claude Code events cmon listens to, with their matcher.
var hookEvents = []struct{ event, matcher string }{
	{"UserPromptSubmit", ""},
	{"PreToolUse", "AskUserQuestion"},
	{"PostToolUse", ""},
	{"Notification", ""},
	{"Stop", ""},
	{"SessionEnd", ""},
}

type hookCommand struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Async   bool     `json:"async,omitempty"`
}

func install() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := editHooks(exe, true); err != nil {
		return err
	}
	if err := installAgent(exe); err != nil {
		return err
	}
	fmt.Println("hooks registered in", settingsPath)
	fmt.Println("login agent installed, the window is starting")
	fmt.Println("sessions already open show up after their next prompt")
	return nil
}

func uninstall() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := editHooks(exe, false); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel)).Run()
	if err := os.Remove(agentPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("hooks and login agent removed; data stays in", monitorDir)
	return nil
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	if strings.Contains(exe, "/go-build") {
		return "", fmt.Errorf("install from a built binary, not go run")
	}
	return exe, nil
}

func isOurs(raw json.RawMessage) bool {
	var h hookCommand
	if json.Unmarshal(raw, &h) != nil {
		return false
	}
	return filepath.Base(h.Command) == "cmon" && len(h.Args) > 0 && h.Args[0] == "hook"
}

// editHooks removes cmon's hooks from settings.json and, when add is true,
// registers them again pointing at exe. Every other key keeps its order.
func editHooks(exe string, add bool) error {
	settings := orderedObject{vals: map[string]json.RawMessage{}}
	original, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := settings.UnmarshalJSON(original); err != nil {
			return fmt.Errorf("%s: %w", settingsPath, err)
		}
	case !os.IsNotExist(err):
		return err
	}

	hooks := orderedObject{vals: map[string]json.RawMessage{}}
	if raw, ok := settings.vals["hooks"]; ok {
		if err := hooks.UnmarshalJSON(raw); err != nil {
			return fmt.Errorf("hooks: %w", err)
		}
	}
	for _, event := range slices.Clone(hooks.keys) { // the loop deletes keys
		var groups []json.RawMessage
		if err := json.Unmarshal(hooks.vals[event], &groups); err != nil {
			return fmt.Errorf("hooks.%s: %w", event, err)
		}
		kept, err := withoutOurs(groups)
		if err != nil {
			return fmt.Errorf("hooks.%s: %w", event, err)
		}
		if len(kept) == 0 {
			hooks.delete(event)
			continue
		}
		hooks.set(event, mustJSON(kept))
	}
	if add {
		for _, e := range hookEvents {
			var groups []json.RawMessage
			if raw, ok := hooks.vals[e.event]; ok {
				_ = json.Unmarshal(raw, &groups)
			}
			// SessionEnd runs as Claude Code exits, so it stays synchronous
			cmd := hookCommand{Type: "command", Command: exe, Args: []string{"hook"}, Async: e.event != "SessionEnd"}
			group := map[string]any{"hooks": []hookCommand{cmd}}
			if e.matcher != "" {
				group = map[string]any{"matcher": e.matcher, "hooks": []hookCommand{cmd}}
			}
			hooks.set(e.event, mustJSON(append(groups, mustJSON(group))))
		}
	}
	if len(hooks.keys) == 0 {
		settings.delete("hooks")
	} else {
		settings.set("hooks", mustJSON(hooks))
	}

	out := mustJSON(settings)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	if bytes.Equal(pretty.Bytes(), original) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	if original != nil {
		if err := os.WriteFile(settingsPath+".bak-cmon", original, 0o644); err != nil {
			return err
		}
	}
	tmp := settingsPath + ".tmp-cmon"
	if err := os.WriteFile(tmp, pretty.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath)
}

// withoutOurs drops cmon's commands from each group, and the groups left empty.
func withoutOurs(groups []json.RawMessage) ([]json.RawMessage, error) {
	var kept []json.RawMessage
	for _, raw := range groups {
		group := orderedObject{vals: map[string]json.RawMessage{}}
		if err := group.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		var cmds []json.RawMessage
		if err := json.Unmarshal(group.vals["hooks"], &cmds); err != nil {
			return nil, err
		}
		var rest []json.RawMessage
		for _, c := range cmds {
			if !isOurs(c) {
				rest = append(rest, c)
			}
		}
		if len(rest) == len(cmds) {
			kept = append(kept, raw)
			continue
		}
		if len(rest) > 0 {
			group.set("hooks", mustJSON(rest))
			kept = append(kept, mustJSON(group))
		}
	}
	return kept, nil
}

func installAgent(exe string) error {
	logPath := filepath.Join(monitorDir, "daemon.log")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, agentLabel, xmlEscape(exe), xmlEscape(logPath))
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(monitorDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(agentPath, []byte(plist), 0o644); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// reinstall replaces a running one; bootout returns before the old one is gone
	service := domain + "/" + agentLabel
	_ = exec.Command("launchctl", "bootout", service).Run()
	for i := 0; i < 50 && exec.Command("launchctl", "print", service).Run() == nil; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if out, err := exec.Command("launchctl", "bootstrap", domain, agentPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// mustJSON marshals without HTML escaping, so commands like "a && b" in
// other hooks stay byte for byte as the user wrote them.
func mustJSON(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// orderedObject is a JSON object that keeps its key order, so editing
// settings.json leaves the rest of the file as the user wrote it.
type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func (o *orderedObject) set(key string, val json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

func (o *orderedObject) delete(key string) {
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *orderedObject) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.New("expected a JSON object")
	}
	o.keys, o.vals = nil, map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return err
		}
		o.set(tok.(string), val)
	}
	_, err := dec.Token()
	return err
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(mustJSON(k))
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
