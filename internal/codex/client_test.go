package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// The subprocess speaks only the protocol; it never launches Codex or reads credentials.
func TestProtocolHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	scenario := os.Args[len(os.Args)-1]
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	var held envelope
	for {
		var m envelope
		if dec.Decode(&m) != nil {
			if scenario == "hang" {
				time.Sleep(time.Hour)
			}
			os.Exit(0)
		}
		switch scenario {
		case "flood":
			for i := 0; i < 1024; i++ {
				enc.Encode(map[string]any{"method": "notice", "params": map[string]int{"n": i}})
			}
		case "reverse":
			if held.ID == nil {
				held = m
				continue
			}
			enc.Encode(map[string]any{"id": m.ID, "result": string(m.Params)})
			enc.Encode(map[string]any{"id": held.ID, "result": string(held.Params)})
			held = envelope{}
			continue
		case "approval":
			enc.Encode(map[string]any{"id": "server-1", "method": "item/commandExecution/requestApproval", "params": map[string]string{"threadId": "thread"}})
			var reply envelope
			if dec.Decode(&reply) != nil {
				os.Exit(2)
			}
			enc.Encode(map[string]any{"id": m.ID, "result": reply.Result})
			continue
		case "timeout":
			if m.Method == "ignore" {
				continue
			}
		case "invalid":
			os.Stdout.WriteString("{bad json}\n")
			continue
		case "exit":
			os.Exit(0)
		}
		enc.Encode(map[string]any{"id": m.ID, "result": string(m.Params)})
	}
}

func fixture(t *testing.T, scenario string) *Client {
	t.Helper()
	c, err := startProcess(os.Args[0], []string{"-test.run=^TestProtocolHelper$", "--", scenario}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func TestNotificationFloodDoesNotBlockResponse(t *testing.T) {
	c := fixture(t, "flood")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result string
	if err := c.Call(ctx, "echo", 42, &result); err != nil || result != "42" {
		t.Fatalf("%q %v", result, err)
	}
	for i := 0; i < 1024; i++ {
		select {
		case e := <-c.Events():
			if e.Method != "notice" {
				t.Fatal(e)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestConcurrentResponsesUseRequestIDs(t *testing.T) {
	c := fixture(t, "reverse")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		go func(v string) {
			var result string
			err := c.Call(ctx, "echo", v, &result)
			expected, _ := json.Marshal(v)
			if err == nil && result != string(expected) {
				err = errors.New("response crossed request IDs")
			}
			errs <- err
		}(value)
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
func TestServerApprovalCancelled(t *testing.T) {
	c := fixture(t, "approval")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result struct {
		Decision string `json:"decision"`
	}
	if err := c.Call(ctx, "trigger", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "cancel" {
		t.Fatal(result)
	}
	select {
	case e := <-c.Events():
		if e.Method != "fuyu/unsupported" {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestTimeoutDoesNotPoisonNextRequest(t *testing.T) {
	c := fixture(t, "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Call(ctx, "ignore", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	var result string
	if err := c.Call(ctx2, "echo", 7, &result); err != nil || result != "7" {
		t.Fatalf("%q %v", result, err)
	}
}
func TestDisconnectReleasesRequestsAndEvents(t *testing.T) {
	for _, scenario := range []string{"invalid", "exit"} {
		t.Run(scenario, func(t *testing.T) {
			c := fixture(t, scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Call(ctx, "trigger", nil, nil); err == nil {
				t.Fatal("expected disconnect")
			}
			select {
			case e := <-c.Events():
				if e.Method != "fuyu/disconnected" {
					t.Fatal(e)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case _, ok := <-c.Events():
				if ok {
					t.Fatal("events still open")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			c.Close()
			c.Close()
		})
	}
}

func TestCloseKillsUnresponsiveOwnedServer(t *testing.T) {
	c := fixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "ready", nil, nil); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("unresponsive server was not cleaned up")
	}
	if c.process.Cmd.ProcessState == nil {
		t.Fatal("owned process was not reaped")
	}
}
