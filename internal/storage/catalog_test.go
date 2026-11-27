package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestModelCatalogFileSharedAcrossCodexFolders(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for index, cwd := range []string{"/first", "/second"} {
			err := store.SaveModelCatalog(Catalog{
				Key: agent.CacheKey{Agent: kind, Cwd: cwd}, Models: []agent.Model{{Model: cwd}},
				Checked: now.Add(time.Duration(index) * time.Minute),
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	records := store.LoadModelCatalogs()
	counts := map[agent.AgentKind]int{}
	for _, record := range records {
		counts[record.Key.Agent]++
		if record.Key.Agent == agent.Codex && record.Models[0].Model != "/second" {
			t.Fatal("stale Codex catalog on disk", record)
		}
	}
	if counts[agent.Codex] != 1 || counts[agent.Claude] != 2 {
		t.Fatal("unexpected model catalog count", counts)
	}
}

func TestCatalogCachePersistsMetadataWithBoundsAndIgnoresDamage(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalogDiskLimit + 3 {
		record := Catalog{
			Key:      agent.CacheKey{Agent: agent.Claude, Runtime: "/runtime", Cwd: fmt.Sprintf("/project/%d", index)},
			Commands: []agent.Command{{Name: "skill", IsSkill: true}},
			Checked:  time.Now(),
		}
		if err := store.SaveCatalog(record); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records := reopened.LoadCatalogs()
	if len(records) != catalogDiskLimit || records[0].Key.Cwd != "/project/34" || records[0].Commands[0].Name != "skill" {
		t.Fatal("metadata was not bounded or restored", len(records))
	}
	cacheDir := filepath.Join(dir, "catalog-cache")
	if err := os.WriteFile(filepath.Join(cacheDir, "broken.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if len(reopened.LoadCatalogs()) != catalogDiskLimit {
		t.Fatal("corrupt cache prevented valid metadata restoration")
	}
	records[0].Skills = []agent.Skill{{Name: string(make([]byte, catalogDiskBytes))}}
	if err := reopened.SaveCatalog(records[0]); err == nil {
		t.Fatal("oversized catalog accepted")
	}
	if len(reopened.LoadCatalogs()) != catalogDiskLimit {
		t.Fatal("failed cache replacement removed previous metadata")
	}
}
