package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	soundAttention = "Ping"  // a session needs you
	soundDone      = "Glass" // a session finished
	soundAllDone   = "Hero"  // the last busy session finished
)

func init() {
	// Cocoa requires the UI on the process main thread, which runs main.main
	runtime.LockOSThread()
}

func runDaemon() error {
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(monitorDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("already running")
		}
		return err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(sessDir); err != nil {
		return err
	}
	go loop(watcher)
	uiRun() // blocks until the window is closed
	return nil
}

type snapshot struct {
	Sessions []view  `json:"sessions"`
	Usage    []limit `json:"usage"`
}

// loop redraws on every session file change. The 1s tick refreshes the
// elapsed times and notices Claude Code processes that died without SessionEnd.
func loop(watcher *fsnotify.Watcher) {
	titles := newTitleCache()
	usage := make(chan []limit)
	go pollUsage(usage)

	var snap snapshot
	prev := map[string]string(nil)
	tick := time.NewTicker(time.Second)
	refresh := func() {
		snap.Sessions = readSessions(titles)
		if prev != nil {
			if s := soundFor(prev, snap.Sessions); s != "" {
				uiPlay(s)
			}
		}
		prev = map[string]string{}
		for _, v := range snap.Sessions {
			prev[v.ID] = v.State
		}
		if b, err := json.Marshal(snap); err == nil {
			uiUpdate(string(b))
		}
	}
	refresh()
	for {
		select {
		case <-watcher.Events:
		case <-watcher.Errors:
		case <-tick.C:
		case snap.Usage = <-usage:
		}
		refresh()
	}
}

// soundFor picks one sound for the state changes between two refreshes.
func soundFor(prev map[string]string, sessions []view) string {
	attention, done, busy := false, false, false
	for _, v := range sessions {
		before, known := prev[v.ID]
		changed := known && before != v.State || !known && v.State != stateWorking
		switch v.State {
		case statePermission, stateQuestion:
			attention = attention || changed
			busy = true
		case stateWorking:
			busy = true
		case stateDone:
			done = done || changed
		}
	}
	switch {
	case attention:
		return soundAttention
	case done && !busy:
		return soundAllDone
	case done:
		return soundDone
	}
	return ""
}

// pollUsage refreshes the usage every 2 minutes, retries sooner on failure
// and backs off when rate limited. A transient failure keeps the last value;
// a rejected login clears it (nil means unavailable).
func pollUsage(out chan<- []limit) {
	client := &http.Client{Timeout: 10 * time.Second}
	for {
		limits, status, err := fetchUsage(client)
		if err != nil {
			log.Print(err) // daemon.log, via the login agent's stderr
		}
		wait := 2 * time.Minute
		switch {
		case err == nil:
			out <- limits
		case status == http.StatusTooManyRequests:
			wait = 5 * time.Minute
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			out <- nil
			wait = 20 * time.Second
		default:
			wait = 20 * time.Second
		}
		time.Sleep(wait)
	}
}
