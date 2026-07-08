package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsExpectedRequest(t *testing.T) {
	var gotPath, gotText string
	var gotChatID any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotChatID = payload["chat_id"]
		gotText, _ = payload["text"].(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient("42:secret", "987654321", srv.URL)
	if err := c.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/bot42:secret/sendMessage" {
		t.Fatalf("path = %q, want /bot42:secret/sendMessage", gotPath)
	}
	// A numeric chat id must be sent as a JSON number, not a string.
	if n, ok := gotChatID.(float64); !ok || n != 987654321 {
		t.Fatalf("chat_id = %v (%T), want numeric 987654321", gotChatID, gotChatID)
	}
	if gotText != "hello" {
		t.Fatalf("text = %q, want hello", gotText)
	}
}

func TestSendUsernameChatIDStaysString(t *testing.T) {
	var gotChatID any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		gotChatID = payload["chat_id"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := NewClient("t", "@mychannel", srv.URL).Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if s, ok := gotChatID.(string); !ok || s != "@mychannel" {
		t.Fatalf("chat_id = %v (%T), want string @mychannel", gotChatID, gotChatID)
	}
}

func TestSendSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	err := NewClient("bad", "1", srv.URL).Send(context.Background(), "x")
	if err == nil {
		t.Fatal("expected an error on a non-200 response")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("error should include the API description, got: %v", err)
	}
}
