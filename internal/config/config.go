// Package config validates server-only authorization and repository configuration.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	Token        string
	Model        string
	Repositories map[string]string
	Enabled      bool
}

func Read(getenv func(string) string) (Config, error) {
	c := Config{Token: getenv("ISSUEPILOT_API_TOKEN"), Model: getenv("ISSUEPILOT_MODEL"), Repositories: map[string]string{}}
	if c.Token != "" && len(c.Token) < 32 {
		return c, errors.New("ISSUEPILOT_API_TOKEN must contain at least 32 bytes")
	}
	raw := getenv("ISSUEPILOT_REPOSITORIES")
	if raw == "" && c.Model == "" {
		return c, nil
	}
	if raw == "" || strings.TrimSpace(c.Model) == "" || c.Token == "" || getenv("OPENAI_API_KEY") == "" || getenv("DATABASE_URL") == "" {
		return c, errors.New("repair mode requires ISSUEPILOT_REPOSITORIES, ISSUEPILOT_MODEL, ISSUEPILOT_API_TOKEN, OPENAI_API_KEY, and DATABASE_URL")
	}
	if err := json.Unmarshal([]byte(raw), &c.Repositories); err != nil || len(c.Repositories) == 0 {
		return c, errors.New("ISSUEPILOT_REPOSITORIES must be a nonempty JSON object of aliases to local absolute repository paths")
	}
	alias := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	for name, path := range c.Repositories {
		if !alias.MatchString(name) || !filepath.IsAbs(path) {
			return c, errors.New("repository aliases must be simple names and paths must be absolute")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return c, errors.New("configured repository does not exist")
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return c, errors.New("configured repository must be a directory")
		}
		if _, err = os.Stat(filepath.Join(resolved, ".git")); err != nil {
			return c, errors.New("configured repository must have Git metadata")
		}
		c.Repositories[name] = resolved
	}
	c.Enabled = true
	return c, nil
}
