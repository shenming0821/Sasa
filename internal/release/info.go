package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var BuildID = "development"
var SourceVersion = "unknown"
var SourceDigest = "unknown"

type Info struct {
	Channel       string `json:"channel"`
	BuildID       string `json:"buildID"`
	SourceVersion string `json:"sourceVersion"`
	SourceDigest  string `json:"sourceDigest"`
	Version       string `json:"version"`
	SchemaVersion int    `json:"schemaVersion"`
}

func Current() (Info, error) {
	executable, err := os.Executable()
	if err != nil {
		return Info{}, err
	}
	return readInfo(executable)
}

func readInfo(executable string) (Info, error) {
	info := Info{Channel: "preview", BuildID: BuildID, SourceVersion: SourceVersion, SourceDigest: SourceDigest, Version: "0.1.0", SchemaVersion: 6}
	contents := filepath.Dir(filepath.Dir(executable))
	if filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return info, nil
	}
	raw, err := os.ReadFile(filepath.Join(contents, "Resources", "release.json"))
	if err != nil {
		return Info{}, fmt.Errorf("read release identity: %w", err)
	}
	info = Info{}
	if err := json.Unmarshal(raw, &info); err != nil {
		return Info{}, fmt.Errorf("read release identity: %w", err)
	}
	if (info.Channel != "stable" && info.Channel != "preview") || info.BuildID != BuildID || info.SourceVersion != SourceVersion || info.SourceDigest != SourceDigest || info.SchemaVersion != 6 || info.Version == "" {
		return Info{}, fmt.Errorf("release identity does not match executable")
	}
	return info, nil
}

func (info Info) Name() string {
	if info.Channel == "preview" {
		return "Fuyu Preview"
	}
	return "Fuyu"
}

func (info Info) WindowTitle() string {
	if info.Channel == "preview" {
		return info.Name() + " · " + info.BuildID
	}
	return info.Name()
}

type Ready struct {
	Info
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	DataDir string `json:"dataDir"`
}

func (info Info) WriteReady(path, token, dataDir string) error {
	if path == "" {
		return nil
	}
	if token == "" {
		return fmt.Errorf("launch token is required with ready-file")
	}
	raw, err := json.Marshal(Ready{Info: info, Token: token, PID: os.Getpid(), DataDir: dataDir})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".ready-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
