// Package telegram implements the `csdd telegram` notifier: a read-only,
// outbound-only bridge that relays plan-run progress and spec-status changes to
// a Telegram chat. Like `csdd web`, it is a *view* over the workspace — it never
// writes artifacts — delivered as chat messages instead of a dashboard.
package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/workspace"
)

// ConfigFile is the notifier config's fixed name under the .csdd/ state dir. It
// holds the bot token (a secret) and the destination chat, so it is written
// 0600 and must be gitignored by the caller (`csdd telegram init`).
const ConfigFile = "bot.json"

// Config is the persisted notifier configuration (.csdd/bot.json).
type Config struct {
	// Token is the Telegram Bot API token from @BotFather. Secret.
	Token string `json:"token"`
	// ChatID is the destination chat: a numeric id ("123456789") or an @username.
	ChatID string `json:"chat_id"`
	// IntervalSeconds is how often the notifier polls the workspace. 0 → default.
	IntervalSeconds int `json:"interval_seconds,omitempty"`
}

// ConfigPath returns <root>/.csdd/bot.json.
func ConfigPath(root string) string {
	return filepath.Join(paths.State(root), ConfigFile)
}

// Load reads .csdd/bot.json. A missing file is surfaced as an actionable error
// pointing at `csdd telegram init`.
func Load(root string) (Config, error) {
	var c Config
	path := ConfigPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, fmt.Errorf("no telegram config at %s — run `csdd telegram init` first", filepath.ToSlash(ConfigPath("")))
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

// Save writes the config atomically at 0600 (it holds a secret token). It also
// creates the .csdd/ directory when absent (via AtomicWrite's MkdirAll).
func Save(root string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return workspace.AtomicWrite(ConfigPath(root), append(data, '\n'), 0o600)
}

// Validate reports the first missing required field.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New("telegram token is empty")
	}
	if strings.TrimSpace(c.ChatID) == "" {
		return errors.New("telegram chat_id is empty")
	}
	return nil
}
