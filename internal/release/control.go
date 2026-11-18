package release

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type quitRequest struct {
	Action     string `json:"action"`
	Executable string `json:"executable"`
}

func StartControl(quit func()) (func(), error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	return listenControl(filepath.Join(cache, "Sasa", "control", fmt.Sprintf("%d.sock", os.Getpid())), executable, quit)
}

func listenControl(path, executable string, quit func()) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("release control path is not a socket: %s", path)
		}
		connection, dialErr := net.DialTimeout("unix", path, time.Second)
		if dialErr == nil {
			connection.Close()
			return nil, fmt.Errorf("release control is already in use: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connection.SetDeadline(time.Now().Add(time.Second))
			var request quitRequest
			err = json.NewDecoder(io.LimitReader(connection, 4096)).Decode(&request)
			if err == nil && request.Action == "quit" && request.Executable == executable {
				quit()
				connection.Write([]byte("accepted\n"))
			} else {
				connection.Write([]byte("rejected\n"))
			}
			connection.Close()
		}
	}()
	return func() { listener.Close(); workers.Wait() }, nil
}
