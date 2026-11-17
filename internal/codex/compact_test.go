package codex

import (
	"context"
	"testing"
	"time"
)

func TestCompactUsesDedicatedRPC(t *testing.T) {
	client := fixture(t, "compact")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.CompactThread(ctx, "thread"); err != nil {
		t.Fatal(err)
	}
}
