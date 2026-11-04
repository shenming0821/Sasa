package power

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type systemBackend struct{}

func (systemBackend) read() (State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/pmset", "-g", "custom").Output()
	if err != nil {
		return State{}, fmt.Errorf("读取系统电源配置：%w", err)
	}
	return parseCustom(string(output))
}

func parseCustom(output string) (State, error) {
	state := State{AC: -1, Battery: -1}
	section := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "AC Power"):
			section = "ac"
		case strings.HasPrefix(line, "Battery Power"):
			section = "battery"
		case strings.HasPrefix(line, "UPS Power"):
			section = ""
		case section != "":
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "sleep" {
				continue
			}
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			if section == "ac" {
				state.AC = value
			} else {
				state.Battery, state.HasBattery = value, true
			}
		}
	}
	if state.AC < 0 {
		return State{}, errors.New("无法解析系统电源配置")
	}
	return state, nil
}

func (systemBackend) write(ctx context.Context, target State) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := fmt.Sprintf("pmset -c sleep %d", target.AC)
	if target.HasBattery {
		command += fmt.Sprintf(" && pmset -b sleep %d", target.Battery)
	}
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, command)
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("更新系统电源配置：%w", ctx.Err())
		}
		if strings.Contains(string(output), "User canceled") {
			return errors.New("授权已取消")
		}
		return fmt.Errorf("更新系统电源配置：%s", strings.TrimSpace(string(output)))
	}
	return nil
}
