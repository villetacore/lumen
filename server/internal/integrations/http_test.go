package integrations

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":         true,
		"1.1.1.1":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"::1":             false,
		"10.1.2.3":        false,
		"192.168.1.10":    false,
		"172.16.0.1":      false,
		"169.254.169.254": false, // cloud metadata
		"100.64.0.1":      false, // carrier-grade NAT
		"198.18.0.1":      false, // benchmarking range
		"0.0.0.0":         false,
		"224.0.0.1":       false,
	}
	for addr, want := range cases {
		if got := IsPublicIP(net.ParseIP(addr)); got != want {
			t.Errorf("IsPublicIP(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestClientRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("должно быть недостижимо"))
	}))
	defer srv.Close()

	client := NewHTTPClient(false)
	_, _, err := Fetch(context.Background(), client, srv.URL, nil)
	if err == nil {
		t.Fatal("a loopback address must be refused")
	}
	if !strings.Contains(err.Error(), "allow_private") {
		t.Errorf("the error should explain the way out, got %v", err)
	}
}

func TestClientAllowsPrivateWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("BEGIN:VCALENDAR\nEND:VCALENDAR\n"))
	}))
	defer srv.Close()

	client := NewHTTPClient(true)
	body, _, err := Fetch(context.Background(), client, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "VCALENDAR") {
		t.Errorf("body = %q", body)
	}
}

func TestClientRefusesNonHTTPSchemes(t *testing.T) {
	client := NewHTTPClient(true)
	_, _, err := Fetch(context.Background(), client, "file:///etc/passwd", nil)
	if err == nil || !strings.Contains(err.Error(), "схема") {
		t.Fatalf("file:// must be refused, got %v", err)
	}
}

func TestFetchSendsConditionalHeaders(t *testing.T) {
	var gotEtag string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEtag = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", `"v2"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	client := NewHTTPClient(true)
	_, resp, err := Fetch(context.Background(), client, srv.URL, map[string]string{"If-None-Match": `"v1"`})
	if err != nil {
		t.Fatal(err)
	}
	if gotEtag != `"v1"` {
		t.Errorf("If-None-Match = %q", gotEtag)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestFetchReportsServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет такого календаря", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewHTTPClient(true)
	_, _, err := Fetch(context.Background(), client, srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "нет такого календаря") {
		t.Fatalf("the server message should surface, got %v", err)
	}
}
