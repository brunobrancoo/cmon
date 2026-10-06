// cmon (claude monitor): floating window with the state of every Claude Code
// session plus the 5h/7d usage limits.
//
//	cmon            run the daemon (the window)
//	cmon hook       called by Claude Code hooks, records a session event
//	cmon install    register the hooks and the login agent
//	cmon uninstall  undo install
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var (
	home       = os.Getenv("HOME")
	monitorDir = filepath.Join(home, ".claude-monitor")
	sessDir    = filepath.Join(monitorDir, "sessions")
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "":
		err = runDaemon()
	case "hook":
		runHook() // never fails: a hook must not disturb Claude Code
	case "install":
		err = install()
	case "uninstall":
		err = uninstall()
	default:
		fmt.Fprintln(os.Stderr, "usage: cmon [hook|install|uninstall]")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cmon:", err)
		os.Exit(1)
	}
}
