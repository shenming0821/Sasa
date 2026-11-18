package ui

import (
	"errors"
	"testing"

	"github.com/shenming0821/Sasa/internal/storage"
)

func TestCompletionDiskCacheShowsPreviousResultWhileRevalidating(t *testing.T) {
	a, backend, jobs := catalogApp(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	a.loadCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	backend.results <- catalogSkills(cwd, "previous")
	pumpUpdate(t, jobs)
	a.catalogWrites.Wait()
	if len(store.LoadCatalogs()) != 1 {
		t.Fatal("successful catalog was not persisted")
	}
	restored := New(backend, "test", cwd, nil)
	restored.store, restored.dispatch = store, a.dispatch
	t.Cleanup(func() { close(restored.shutdown); restored.catalogWrites.Wait() })
	restored.restoreCompletionCatalogs()
	restored.loadCompletionSkills()
	awaitCatalog(t, backend, true)
	if !restored.completion.loading || len(restored.completion.skills) != 1 || restored.completion.skills[0].Name != "previous" {
		t.Fatal("cached candidates disappeared while native validation was pending")
	}
	backend.results <- catalogResult{err: errors.New("offline")}
	pumpUpdate(t, jobs)
	if restored.completion.skills[0].Name != "previous" || restored.completion.error == "" {
		t.Fatal("failed refresh discarded last successful metadata")
	}
	records := store.LoadCatalogs()
	if len(records) != 1 || records[0].Skills[0].Name != "previous" {
		t.Fatal("failed refresh overwrote disk cache")
	}
	restored.loadCompletionCatalog(true)
	awaitCatalog(t, backend, true)
	backend.results <- catalogSkills(cwd, "current")
	pumpUpdate(t, jobs)
	restored.catalogWrites.Wait()
	if restored.completion.skills[0].Name != "current" || store.LoadCatalogs()[0].Skills[0].Name != "current" {
		t.Fatal("successful refresh did not replace stale metadata")
	}
}
