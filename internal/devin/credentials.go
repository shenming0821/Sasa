package devin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveExecutable locates the devin CLI binary.
func ResolveExecutable() (string, error) {
	if path, err := exec.LookPath("devin"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, path := range []string{filepath.Join(home, ".local/bin/devin"), "/opt/homebrew/bin/devin", "/usr/local/bin/devin"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 Devin，请安装 Devin CLI 后重新启动 Fuyu")
}

// Home is the runtime identity: the directory holding Devin's local state and
// session store, matching the CLI's own data root.
func Home() (string, error) {
	if dir := os.Getenv("DEVIN_DATA_DIR"); dir != "" {
		path, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "devin"), nil
}

// ResolveAPIKey reads the key passed to ACP authenticate. The environment wins
// so a service-account key can override an interactive login; otherwise the
// CLI/Desktop credential file provides the same key `devin auth` stores.
func ResolveAPIKey() (string, error) {
	if key := strings.TrimSpace(os.Getenv("DEVIN_API_KEY")); key != "" {
		return key, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(home, "credentials.toml"))
	if err != nil {
		return "", fmt.Errorf("未找到 Devin 凭据，请设置 DEVIN_API_KEY 或登录 Devin：%w", err)
	}
	if key := credentialsValue(string(raw), "windsurf_api_key"); key != "" {
		return key, nil
	}
	return "", fmt.Errorf("Devin 凭据缺少 API key，请设置 DEVIN_API_KEY 或重新登录 Devin")
}

// credentialsValue parses the flat key = "value" pairs the CLI credential file
// uses; it is not a general TOML parser.
func credentialsValue(raw, name string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, name) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, name))
		if !strings.HasPrefix(value, "=") {
			continue
		}
		value = strings.TrimSpace(strings.TrimPrefix(value, "="))
		return strings.Trim(value, `"'`)
	}
	return ""
}
