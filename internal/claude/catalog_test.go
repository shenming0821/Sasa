package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandCatalogIdentifiesInvocableSkills(t *testing.T) {
	client := fixtureClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cwd := t.TempDir()
	commands, err := client.ListCommands(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 5 {
		t.Fatal(commands)
	}
	for _, command := range commands {
		expectedSkill := command.Name == "frontend-design" || command.Name == "winning_trace_analysis"
		if command.IsSkill != expectedSkill || command.Cwd != cwd {
			t.Fatalf("incorrect skill classification or directory: %+v", command)
		}
		if command.Name == "frontend-design" && (command.Description != "Design interfaces" || len(command.Arguments) != 1 || command.Arguments[0] != "[page]") {
			t.Fatalf("skill command metadata lost: %+v", command)
		}
	}
}

func TestCommandCatalogDoesNotFallBackToActions(t *testing.T) {
	for _, scenario := range []string{"skills-unavailable", "skills-empty"} {
		t.Run(scenario, func(t *testing.T) {
			client := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cwd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cwd, scenario), nil, 0600); err != nil {
				t.Fatal(err)
			}
			commands, err := client.ListCommands(ctx, cwd)
			if scenario == "skills-unavailable" {
				if err == nil || !strings.Contains(err.Error(), "get_skills_dialog unavailable") || len(commands) != 0 {
					t.Fatalf("skill catalog failure hidden: commands=%+v err=%v", commands, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range commands {
				if command.IsSkill {
					t.Fatalf("empty skill catalog exposed action: %+v", command)
				}
			}
		})
	}
}

func TestLiveClaudeSkillCommandCatalog(t *testing.T) {
	if os.Getenv("SASA_LIVE_CLAUDE") != "1" {
		t.Skip("set SASA_LIVE_CLAUDE=1 for real skill catalog verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cwd := t.TempDir()
	skillDirectory := filepath.Join(cwd, ".claude", "skills", "sasa-catalog-probe")
	if err := os.MkdirAll(skillDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte("---\nname: sasa-catalog-probe\ndescription: Sasa isolated catalog verification\ndisable-model-invocation: true\n---\nReturn the requested text.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	commands, err := client.ListCommands(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	found, skillCount := false, 0
	for _, command := range commands {
		if command.IsSkill {
			skillCount++
		}
		if command.Name == "sasa-catalog-probe" && command.IsSkill {
			found = true
		}
		if (command.Name == "model" || command.Name == "compact") && command.IsSkill {
			t.Fatalf("native action classified as skill: %s", command.Name)
		}
	}
	if !found {
		t.Fatal("temporary project skill missing from native catalog")
	}
	t.Logf("native catalog: commands=%d skills=%d; isolated project skill found", len(commands), skillCount)
}
