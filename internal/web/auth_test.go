package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthDisabledAllowsAll(t *testing.T) {
	a := newAuth(false, "")
	if !a.ok(httptest.NewRequest("GET", "/api/overview", nil)) {
		t.Error("disabled auth should allow any request")
	}
}

func TestAuthTokenSources(t *testing.T) {
	a := newAuth(true, "s3cret")
	if a.token != "s3cret" {
		t.Fatalf("token = %q, want s3cret", a.token)
	}
	if a.ok(httptest.NewRequest("GET", "/api/overview", nil)) {
		t.Error("missing token should be rejected")
	}

	bearer := httptest.NewRequest("GET", "/api/overview", nil)
	bearer.Header.Set("Authorization", "Bearer s3cret")
	if !a.ok(bearer) {
		t.Error("valid bearer should pass")
	}

	wrong := httptest.NewRequest("GET", "/api/overview", nil)
	wrong.Header.Set("Authorization", "Bearer nope")
	if a.ok(wrong) {
		t.Error("wrong bearer should fail")
	}

	cookie := httptest.NewRequest("GET", "/api/overview", nil)
	cookie.AddCookie(&http.Cookie{Name: tokenCookie, Value: "s3cret"})
	if !a.ok(cookie) {
		t.Error("valid cookie should pass")
	}

	if !a.ok(httptest.NewRequest("GET", "/api/events?token=s3cret", nil)) {
		t.Error("valid ?token should pass")
	}
}

func TestAuthProtect(t *testing.T) {
	a := newAuth(true, "k")
	h := a.protect(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/api/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token → %d, want 401", w.Code)
	}

	w2 := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/overview", nil)
	r.Header.Set("Authorization", "Bearer k")
	h(w2, r)
	if w2.Code != http.StatusOK {
		t.Errorf("valid token → %d, want 200", w2.Code)
	}
}

func TestRandomToken(t *testing.T) {
	if randomToken() == randomToken() {
		t.Error("tokens should be random")
	}
	if len(randomToken()) < 8 {
		t.Error("token too short")
	}
}

func TestMuxAuthEnforced(t *testing.T) {
	root := tempWorkspace(t, map[string]string{"specs/f/spec.json": `{"phase":"x"}`})
	srv := httptest.NewServer(newMux(root, newHub(), newAuth(true, "tok")))
	defer srv.Close()

	cases := []struct {
		path string
		want int
	}{
		{"/api/health", 200},   // public
		{"/", 200},             // SPA public
		{"/api/overview", 401}, // protected, no token
		{"/api/overview?token=tok", 200},
		{"/api/tree?token=tok", 200},
	}
	for _, c := range cases {
		resp, err := http.Get(srv.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s → %d, want %d", c.path, resp.StatusCode, c.want)
		}
	}
}
