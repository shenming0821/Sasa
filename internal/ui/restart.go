package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

func (a *App) restartApp() {
	if a.restarting || a.quitting || a.quitDialog || a.quitStopping {
		return
	}
	command, err := a.replacementCommand()
	if err != nil {
		a.State.Error = "重启失败：" + err.Error()
		return
	}
	a.requestRestart(func() error {
		if err := command.Start(); err != nil {
			return err
		}
		return command.Process.Release()
	})
}

func (a *App) requestRestart(launch func() error) {
	a.restarting, a.restartLaunch = true, launch
	a.quitApp()
}

func (a *App) cancelRestart() {
	a.restarting, a.restartLaunch = false, nil
}

func (a *App) finishRestart() error {
	if !a.restarting || !a.quitting || a.restartLaunch == nil {
		return nil
	}
	launch := a.restartLaunch
	a.cancelRestart()
	return launch()
}

func (a *App) replacementCommand() (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
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
	launch, err := replacementArguments(executable, args)
	if err != nil {
		return nil, err
	}
	command := waitForExitCommand(os.Getpid(), launch)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command, nil
}

func replacementArguments(executable string, arguments []string) ([]string, error) {
	if runtime.GOOS != "darwin" {
		return append([]string{executable}, arguments...), nil
	}
	contents := filepath.Dir(filepath.Dir(executable))
	application := filepath.Dir(contents)
	if filepath.Base(filepath.Dir(executable)) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(application) != ".app" {
		return nil, fmt.Errorf("请从 Sasa 应用包启动后重启")
	}
	launch := []string{"/usr/bin/open", "-n", "-a", application}
	for _, name := range []string{"PATH", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		if value, exists := os.LookupEnv(name); exists {
			launch = append(launch, "--env", name+"="+value)
		}
	}
	return append(append(launch, "--args"), arguments...), nil
}

func waitForExitCommand(parentPID int, launch []string) *exec.Cmd {
	script := `parent_pid=$1
shift
attempts=0
while /bin/kill -0 "$parent_pid" 2>/dev/null; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 300 ]; then exit 1; fi
    /bin/sleep 0.1
done
exec "$@"`
	arguments := append([]string{"-c", script, "fuyu-restart", strconv.Itoa(parentPID)}, launch...)
	return exec.Command("/bin/sh", arguments...)
}
