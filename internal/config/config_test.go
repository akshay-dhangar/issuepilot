package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairRequiresCompleteConfiguration(t *testing.T) {
	env := map[string]string{"ISSUEPILOT_MODEL": "model"}
	get := func(k string) string { return env[k] }
	if _, err := Read(get); err == nil {
		t.Fatal("partial configuration accepted")
	}
	env = map[string]string{}
	c, err := Read(get)
	if err != nil || c.Enabled {
		t.Fatal("demo mode failed")
	}
	env["ISSUEPILOT_API_TOKEN"] = "short"
	if _, err = Read(get); err == nil {
		t.Fatal("weak token accepted")
	}
}
func TestConfiguredRepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"ISSUEPILOT_MODEL": "test-model", "ISSUEPILOT_API_TOKEN": strings.Repeat("x", 32), "OPENAI_API_KEY": "test-key", "DATABASE_URL": "test-db", "ISSUEPILOT_REPOSITORIES": `{"sample":"` + dir + `"}`}
	c, err := Read(func(k string) string { return env[k] })
	if err != nil || !c.Enabled || c.Repositories["sample"] == "" {
		t.Fatalf("%+v %v", c, err)
	}
	env["ISSUEPILOT_REPOSITORIES"] = `{"sample":"relative"}`
	if _, err = Read(func(k string) string { return env[k] }); err == nil {
		t.Fatal("relative path accepted")
	}
}
