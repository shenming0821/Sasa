package release

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlValidatesTargetAndCleansUpSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "fuyu-ctl-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "control.sock")
	var requests atomic.Int32
	stop, err := listenControl(path, "/app/Fuyu", func() { requests.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := listenControl(path, "/app/Fuyu", func() {}); err == nil {
		t.Fatal("replaced an active listener")
	}
	for _, test := range []struct {
		request quitRequest
		want    string
	}{
		{quitRequest{Action: "quit", Executable: "/other/Fuyu"}, "rejected\n"},
		{quitRequest{Action: "force-quit", Executable: "/app/Fuyu"}, "rejected\n"},
		{quitRequest{Action: "quit", Executable: "/app/Fuyu"}, "accepted\n"},
	} {
		connection, err := net.DialTimeout("unix", path, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		connection.SetDeadline(time.Now().Add(time.Second))
		if err := json.NewEncoder(connection).Encode(test.request); err != nil {
			t.Fatal(err)
		}
		response, err := bufio.NewReader(connection).ReadString('\n')
		connection.Close()
		if err != nil || response != test.want {
			t.Fatalf("response: %q, %v", response, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("quit requests: %d", requests.Load())
	}
	stop()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("socket survived listener close")
	}
}
