package ui

import (
	"os"
	"os/exec"
	"path/filepath"
)

// restartApp relaunches the same executable with the current data directory
// and working directory, then exits so the new process replaces this one.
// It is intended as a quick manual restart while iterating on Fuyu itself.
func (a *App) restartApp() {
	if a.restarting {
		return
	}
	a.restarting = true
	if err := a.spawnReplacement(); err != nil {
		a.restarting = false
		a.State.Error = "重启失败：" + err.Error()
		return
	}
	os.Exit(0)
}

func (a *App) spawnReplacement() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	args := []string{}
	if cwd := a.workspaceCwd(); cwd != "" {
		args = append(args, "-cwd", cwd)
	}
	if a.store != nil {
		args = append(args, "-data-dir", a.store.Directory())
	}
	cmd := exec.Command(executable, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
