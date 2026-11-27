package storage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

const catalogDiskLimit = 32
const catalogDiskBytes = 1 << 20

type Catalog struct {
	Key      agent.CacheKey
	Skills   []agent.Skill
	Commands []agent.Command
	Models   []agent.Model `json:",omitempty"`
	Checked  time.Time
}

// Catalogs are replaceable metadata. Corrupt, oversized or foreign entries
// never prevent the application index from opening.
func (s *Store) LoadCatalogs() []Catalog {
	return s.loadCatalogs("catalog-cache")
}

func (s *Store) LoadModelCatalogs() []Catalog {
	return s.loadCatalogs("model-catalog-cache")
}

func (s *Store) loadCatalogs(cacheDir string) []Catalog {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	dir := filepath.Join(s.dir, cacheDir)
	files, _ := os.ReadDir(dir)
	var records []Catalog
	for _, file := range files {
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > catalogDiskBytes || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		var record Catalog
		if json.Unmarshal(raw, &record) != nil || record.Key.Agent.Validate() != nil || !filepath.IsAbs(record.Key.Cwd) || record.Checked.IsZero() {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Checked.After(records[j].Checked) })
	return records[:min(len(records), catalogDiskLimit)]
}

func (s *Store) SaveCatalog(record Catalog) error {
	return s.saveCatalog("catalog-cache", record)
}

func (s *Store) SaveModelCatalog(record Catalog) error {
	return s.saveCatalog("model-catalog-cache", record)
}

func (s *Store) saveCatalog(cacheDir string, record Catalog) error {
	if record.Key.Agent.Validate() != nil || !filepath.IsAbs(record.Key.Cwd) || record.Checked.IsZero() {
		return fmt.Errorf("目录缓存身份或时间无效")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw) > catalogDiskBytes {
		return fmt.Errorf("目录缓存超过 1 MiB")
	}
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	dir := filepath.Join(s.dir, cacheDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	key, _ := json.Marshal(record.Key)
	path := filepath.Join(dir, fmt.Sprintf("%x.json", sha256.Sum256(key)))
	if len(record.Models) > 0 {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(record.Checked) {
			return nil
		}
	}
	file, err := os.CreateTemp(dir, ".catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(record.Models) > 0 {
		if err := os.Chtimes(file.Name(), record.Checked, record.Checked); err != nil {
			return err
		}
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	files, _ := os.ReadDir(dir)
	type candidate struct {
		name string
		time time.Time
	}
	var candidates []candidate
	for _, entry := range files {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && strings.HasSuffix(entry.Name(), ".json") {
			candidates = append(candidates, candidate{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].time.After(candidates[j].time) })
	for _, entry := range candidates[min(len(candidates), catalogDiskLimit):] {
		_ = os.Remove(filepath.Join(dir, entry.name))
	}
	return nil
}
