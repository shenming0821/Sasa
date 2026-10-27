package codex

import (
	"context"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"os"
	"testing"
	"time"
)

func TestSkillsListAndExplicitSkillInput(t *testing.T) {
	c := fixture(t, "skills")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entries, err := c.ListSkills(ctx, "/workspace")
	if err != nil || len(entries) != 1 || len(entries[0].Skills) != 1 || !entries[0].Skills[0].Enabled || len(entries[0].Errors) != 1 {
		t.Fatalf("list: %+v %v", entries, err)
	}
	turn, err := c.SendWithSkills(ctx, "thread", "$design task", entries[0].Skills)
	if err != nil || turn.ID != "turn" {
		t.Fatalf("turn: %+v %v", turn, err)
	}
}

// This opt-in reads installed skills only; it never creates a thread or turn.
func TestLiveSkillsReadOnly(t *testing.T) {
	if os.Getenv("FUYU_LIVE_SKILLS") != "1" {
		t.Skip("set FUYU_LIVE_SKILLS=1 for native read-only skills verification")
	}
	cwd := os.Getenv("FUYU_SKILLS_CWD")
	if cwd == "" {
		t.Fatal("set FUYU_SKILLS_CWD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Start(ctx, "codex", cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	entries, err := c.ListSkills(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Cwd != cwd {
			continue
		}
		found = true
		enabled := 0
		for _, skill := range entry.Skills {
			if skill.Enabled {
				enabled++
			}
			if skill.Name == "" || skill.Path == "" {
				t.Fatal("missing native skill identity")
			}
		}
		t.Logf("cwd=%s skills=%d enabled=%d errors=%d", entry.Cwd, len(entry.Skills), enabled, len(entry.Errors))
		for _, issue := range entry.Errors {
			t.Errorf("skill error path=%s message=%s", issue.Path, issue.Message)
		}
	}
	if !found {
		t.Fatal("missing current-directory result")
	}
}

func TestImagesAndSkillsShareTurnInput(t *testing.T) {
	c := fixture(t, "images")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	turn, err := c.SendWithImages(ctx, "thread", "请看图", []Skill{{Name: "design", Path: "/skills/SKILL.md"}}, []attachment.Image{{Path: "/owned/截图.png"}, {URL: "data:image/png;base64,fixture"}})
	if err != nil || turn.ID != "image-turn" {
		t.Fatal("mixed image input rejected", turn, err)
	}
}
