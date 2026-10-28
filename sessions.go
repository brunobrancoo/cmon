package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const (
	stateWorking    = "working"
	stateDone       = "done"
	statePermission = "permission"
	stateQuestion   = "question"
)

// session is the file the hook writes to sessions/<id>.json. Times in unix ms.
type session struct {
	State      string `json:"state"`
	Since      int64  `json:"since"`
	Updated    int64  `json:"updated"`
	PID        int    `json:"pid"`
	Cwd        string `json:"cwd"`
	Transcript string `json:"transcript"`
}

// view is one row of the window.
type view struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Since int64  `json:"since"`
}

// readSessions loads the session files, drops the ones whose Claude Code
// process is gone and keeps one session per process (/clear starts a new
// session id in the same process).
func readSessions(titles *titleCache) []view {
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil
	}
	type loaded struct {
		id string
		s  session
	}
	var all []loaded
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		file := filepath.Join(sessDir, e.Name())
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var s session
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		if !pidAlive(s.PID) {
			os.Remove(file)
			continue
		}
		all = append(all, loaded{id, s})
	}

	sort.Slice(all, func(i, j int) bool { return all[i].s.Updated > all[j].s.Updated })
	seen := map[int]bool{}
	var views []view
	live := map[string]bool{}
	for _, l := range all {
		if seen[l.s.PID] {
			os.Remove(filepath.Join(sessDir, l.id+".json"))
			continue
		}
		seen[l.s.PID] = true
		live[l.s.Transcript] = true
		name := titles.get(l.s.Transcript)
		if name == "" {
			name = filepath.Base(l.s.Cwd)
		}
		views = append(views, view{ID: l.id, Name: name, State: effectiveState(l.s), Since: l.s.Since})
	}
	titles.forgetExcept(live)
	// newest state change first: the row with the smallest elapsed time is on top
	sort.SliceStable(views, func(i, j int) bool { return views[i].Since > views[j].Since })
	return views
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// effectiveState covers what the hooks miss: after a permission prompt or a
// question is answered, Claude Code may write to the transcript well before
// the next hook fires, which means the session is working again.
func effectiveState(s session) string {
	if s.State != statePermission && s.State != stateQuestion {
		return s.State
	}
	info, err := os.Stat(s.Transcript)
	if err == nil && info.ModTime().UnixMilli() > s.Since+2000 {
		return stateWorking
	}
	return s.State
}

// titleCache reads the session title from the transcript, scanning only the
// bytes appended since the previous read (transcripts grow to many MB).
// A /rename ("custom-title") wins over the generated one ("ai-title").
type titleCache struct {
	entries map[string]*titleEntry
}

type titleEntry struct {
	offset     int64
	custom, ai string
}

func newTitleCache() *titleCache { return &titleCache{entries: map[string]*titleEntry{}} }

func (c *titleCache) get(path string) string {
	if path == "" {
		return ""
	}
	e := c.entries[path]
	if e == nil {
		e = &titleEntry{}
		c.entries[path] = e
	}
	c.scan(path, e)
	if e.custom != "" {
		return e.custom
	}
	return e.ai
}

func (c *titleCache) scan(path string, e *titleEntry) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	if info.Size() < e.offset { // rewritten
		*e = titleEntry{}
	}
	if info.Size() == e.offset {
		return
	}
	if _, err := f.Seek(e.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // partial last line: read it again next time
			return
		}
		e.offset += int64(len(line))
		if !bytes.Contains(line, []byte(`-title"`)) {
			continue
		}
		var rec struct {
			Type        string `json:"type"`
			AITitle     string `json:"aiTitle"`
			CustomTitle string `json:"customTitle"`
		}
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		switch rec.Type {
		case "custom-title":
			e.custom = rec.CustomTitle
		case "ai-title":
			e.ai = rec.AITitle
		}
	}
}

func (c *titleCache) forgetExcept(live map[string]bool) {
	for path := range c.entries {
		if !live[path] {
			delete(c.entries, path)
		}
	}
}
