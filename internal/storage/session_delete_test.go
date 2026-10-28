package storage

import (
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestDeleteSessionCommitsIndexAndHistoryFilterTogether(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := Snapshot{Sessions: []Session{{ID: 1, Cwd: "/one"}, {ID: 2, Cwd: "/two"}}}
	if err := store.Save(original); err != nil {
		t.Fatal(err)
	}
	deleted := DeletedThread{Agent: agent.Claude, Runtime: "isolated", ThreadID: "thread"}
	invalid := Snapshot{DeletedThreads: []DeletedThread{deleted}, Sessions: []Session{{ID: 2, Agent: "invalid"}}}
	if err := store.DeleteSession(invalid, 1); err == nil {
		t.Fatal("invalid snapshot committed")
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Sessions) != 2 || len(loaded.DeletedThreads) != 0 {
		t.Fatalf("failed deletion was not atomic: %+v %v", loaded, err)
	}
	valid := Snapshot{DeletedThreads: []DeletedThread{deleted}, Sessions: original.Sessions[1:]}
	if err := store.DeleteSession(valid, 1); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load()
	if err != nil || len(loaded.Sessions) != 1 || loaded.Sessions[0].ID != 2 || len(loaded.DeletedThreads) != 1 || loaded.DeletedThreads[0] != deleted {
		t.Fatalf("deletion did not persist: %+v %v", loaded, err)
	}
}
