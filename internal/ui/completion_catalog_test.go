package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
)

type catalogRequest struct {
	cwd   string
	force bool
}

type catalogResult struct {
	entries []agent.SkillsEntry
	err     error
}

type catalogTestBackend struct {
	stubBackend
	requests chan catalogRequest
	results  chan catalogResult
	stopped  chan struct{}
}

func (backend *catalogTestBackend) ListSkills(ctx context.Context, cwd string) ([]agent.SkillsEntry, error) {
	return backend.read(ctx, cwd, false)
}

func (backend *catalogTestBackend) ReloadSkills(ctx context.Context, cwd string) ([]agent.SkillsEntry, error) {
	return backend.read(ctx, cwd, true)
}

func (backend *catalogTestBackend) read(ctx context.Context, cwd string, force bool) ([]agent.SkillsEntry, error) {
	defer func() {
		if ctx.Err() != nil && backend.stopped != nil {
			backend.stopped <- struct{}{}
		}
	}()
	select {
	case backend.requests <- catalogRequest{cwd: cwd, force: force}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-backend.results:
		return result.entries, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func catalogApp(t *testing.T) (*App, *catalogTestBackend, chan func()) {
	t.Helper()
	backend := &catalogTestBackend{requests: make(chan catalogRequest, 8), results: make(chan catalogResult, 8), stopped: make(chan struct{}, 8)}
	application := New(backend, "test", "/project", nil)
	jobs := make(chan func(), 16)
	application.dispatch = func(job func()) { jobs <- job }
	t.Cleanup(func() {
		select {
		case <-application.shutdown:
		default:
			close(application.shutdown)
		}
	})
	return application, backend, jobs
}

func awaitCatalog(t *testing.T, backend *catalogTestBackend, force bool) string {
	t.Helper()
	select {
	case request := <-backend.requests:
		if request.force != force {
			t.Fatalf("force=%v, want %v", request.force, force)
		}
		return request.cwd
	case <-time.After(time.Second):
		t.Fatal("catalog request missing")
		return ""
	}
}

func catalogSkills(cwd, name string) catalogResult {
	return catalogResult{entries: []agent.SkillsEntry{{Cwd: cwd, Skills: []agent.Skill{{Name: name, Path: cwd + "/SKILL.md", Enabled: true}}}}}
}

func TestCatalogPrewarmsWithoutInputAndSharesInFlightAcrossSessions(t *testing.T) {
	application, backend, jobs := catalogApp(t)
	application.prewarmCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	first := application.session
	application.newSession(cwd)
	application.loadCompletionSkills()
	if !application.completion.loading || application.State.Draft != "" || len(backend.requests) != 0 {
		t.Fatal("preload changed input or duplicated in-flight request")
	}
	backend.results <- catalogSkills(cwd, "design")
	pumpUpdate(t, jobs)
	if len(first.completion.skills) != 1 || len(application.completion.skills) != 1 {
		t.Fatal("catalog did not reach matching session owners")
	}
	application.newSession(cwd)
	application.loadCompletionSkills()
	if len(application.completion.skills) != 1 || len(backend.requests) != 0 {
		t.Fatal("new session did not immediately reuse cached list")
	}
	application.newSession("/other")
	application.loadCompletionSkills()
	if len(application.completion.skills) != 0 || awaitCatalog(t, backend, false) != "/other" {
		t.Fatal("catalog crossed working directories")
	}
	backend.results <- catalogSkills("/other", "other")
	pumpUpdate(t, jobs)
}

func TestCatalogRefreshKeepsCandidatesOnFailureAndRetriesExplicitly(t *testing.T) {
	application, backend, jobs := catalogApp(t)
	application.prewarmCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	backend.results <- catalogSkills(cwd, "old")
	pumpUpdate(t, jobs)
	entry := application.completionCatalogs[application.completionCacheKey()]
	entry.checked = time.Now().Add(-completionCatalogTTL)
	application.loadCompletionSkills()
	awaitCatalog(t, backend, true)
	if len(application.completion.skills) != 1 || application.completion.skills[0].Name != "old" {
		t.Fatal("stale candidates disappeared during refresh")
	}
	backend.results <- catalogResult{err: errors.New("temporary failure")}
	pumpUpdate(t, jobs)
	application.loadCompletionSkills()
	if len(backend.requests) != 0 || application.completion.error == "" || len(application.completion.skills) != 1 {
		t.Fatal("failed refresh lost candidates or retried every frame")
	}
	application.loadCompletionCatalog(true)
	awaitCatalog(t, backend, true)
	backend.results <- catalogSkills(cwd, "new")
	pumpUpdate(t, jobs)
	if application.completion.error != "" || application.completion.skills[0].Name != "new" {
		t.Fatal("explicit refresh did not replace old catalog")
	}
	application.loadCompletionCatalog(true)
	awaitCatalog(t, backend, true)
	backend.results <- catalogResult{entries: []agent.SkillsEntry{{Cwd: cwd}}}
	pumpUpdate(t, jobs)
	if len(application.completion.skills) != 0 {
		t.Fatal("successful empty catalog retained removed skills")
	}
}

func TestCatalogSeparatesRuntimeAndInvalidatesChangedSkills(t *testing.T) {
	application, backend, jobs := catalogApp(t)
	alternate := &catalogTestBackend{requests: backend.requests, results: backend.results}
	if err := application.RegisterBackend(agent.Codex, "alternate", alternate); err != nil {
		t.Fatal(err)
	}
	application.prewarmCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	backend.results <- catalogSkills(cwd, "original")
	pumpUpdate(t, jobs)
	application.runtimeKey = "alternate"
	application.loadCompletionSkills()
	awaitCatalog(t, backend, false)
	backend.results <- catalogSkills(cwd, "alternate")
	pumpUpdate(t, jobs)
	if len(application.completionCatalogs) != 2 || application.completion.skills[0].Name != "alternate" {
		t.Fatal("runtime catalogs were mixed")
	}
	application.applyWorkspaceChanges(application.workspace(cwd), map[string]bool{cwd + "/.agents/skills/test/SKILL.md": true})
	application.loadCompletionSkills()
	awaitCatalog(t, backend, true)
	backend.results <- catalogSkills(cwd, "changed")
	pumpUpdate(t, jobs)
}

func TestCatalogPrewarmSkipsBusyAndDisconnectAndCancelsOnShutdown(t *testing.T) {
	application, backend, _ := catalogApp(t)
	application.State.Busy = true
	application.prewarmCompletionSkills()
	application.State.Busy, application.State.Disconnected = false, true
	application.prewarmCompletionSkills()
	if len(application.completionCatalogs) != 0 {
		t.Fatal("busy or disconnected application started catalog work")
	}
	application.State.Disconnected = false
	application.prewarmCompletionSkills()
	awaitCatalog(t, backend, false)
	close(application.shutdown)
	select {
	case <-backend.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel owned work")
	}
}

func TestCatalogEvictsOldestCompletedEntryAndBoundsInFlightWork(t *testing.T) {
	application, _, _ := catalogApp(t)
	application.completionCatalogs = make(map[agent.CacheKey]*completionCatalog)
	oldest := agent.CacheKey{Agent: agent.Codex, Cwd: "/oldest"}
	application.completionCatalogs[oldest] = &completionCatalog{used: time.Now().Add(-time.Hour)}
	for index := 1; index < completionCatalogLimit; index++ {
		key := agent.CacheKey{Agent: agent.Codex, Cwd: fmt.Sprintf("/workspace/%d", index)}
		application.completionCatalogs[key] = &completionCatalog{used: time.Now(), loading: true}
	}
	application.trimCompletionCatalogs()
	if len(application.completionCatalogs) != completionCatalogLimit-1 || application.completionCatalogs[oldest] != nil {
		t.Fatal("completed LRU entry was not evicted")
	}
	application.prewarmCompletionSkills()
	if application.completionCatalogs[application.completionCacheKey()] != nil {
		t.Fatal("idle preload overlapped an existing catalog process")
	}
}

func TestCatalogManualRefreshKeepsInputFocusAndCachedCandidates(t *testing.T) {
	application, backend, jobs := catalogApp(t)
	application.prewarmCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	backend.results <- catalogSkills(cwd, "design")
	pumpUpdate(t, jobs)
	tester := native.NewTester(application.view, 900, 700)
	if err := tester.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tester.Type("$")
	if err := tester.Click("刷新技能"); err != nil {
		t.Fatal(err)
	}
	awaitCatalog(t, backend, true)
	tester.Type("des")
	if application.State.Draft != "$des" || !tester.HasText("design") {
		t.Fatal("refresh stole input focus or hid cached candidates")
	}
	backend.results <- catalogSkills(cwd, "design-new")
	pumpUpdate(t, jobs)
	tester.Frame()
	if _, found := tester.Find("候选 design-new"); !found {
		t.Fatal("refreshed candidate not visible")
	}
}
