package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// defaultAPIBase is the public Telegram Bot API root. Overridable per-client so
// tests can point at an httptest server.
const defaultAPIBase = "https://api.telegram.org"

// Client sends messages to one chat via the Telegram Bot API. It is outbound
// only — it never reads updates — matching the notifier's read-only contract.
type Client struct {
	token   string
	chatID  string
	baseURL string
	http    *http.Client
}

// NewClient builds a client for the given token/chat. An empty baseURL falls
// back to the public API; tests inject an httptest server URL.
func NewClient(token, chatID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultAPIBase
	}
	return &Client{
		token:   token,
		chatID:  chatID,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// chatIDValue renders the chat id as the Bot API expects: a JSON number for a
// numeric id, or a string for an @username.
func chatIDValue(chatID string) any {
	if n, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		return n
	}
	return chatID
}

// Send posts one plain-text message. No parse mode is set, so arbitrary text
// (Markdown-ish log lines, arrows, emoji) is delivered verbatim without the API
// rejecting unbalanced formatting.
func (c *Client) Send(ctx context.Context, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id": chatIDValue(c.chatID),
		"text":    text,
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram sendMessage: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}
