package emby

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshLibrarySendsExpectedRequest(t *testing.T) {
	requestSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true

		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/emby/Items/movie-library-id/Refresh" {
			t.Fatalf("path = %s, want /emby/Items/movie-library-id/Refresh", r.URL.Path)
		}
		if got := r.URL.Query().Get("Recursive"); got != "true" {
			t.Fatalf("Recursive query = %s, want true", got)
		}
		if got := r.Header.Get("X-Emby-Token"); got != "test-token" {
			t.Fatalf("X-Emby-Token = %s, want test-token", got)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL + "/", APIKey: "test-token", HTTPClient: server.Client()}
	if err := client.RefreshLibrary(context.Background(), "movie-library-id"); err != nil {
		t.Fatalf("RefreshLibrary returned error: %v", err)
	}
	if !requestSeen {
		t.Fatal("server did not receive request")
	}
}

func TestRefreshLibraryReturnsErrorForNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, APIKey: "test-token", HTTPClient: server.Client()}
	if err := client.RefreshLibrary(context.Background(), "movie-library-id"); err == nil {
		t.Fatal("RefreshLibrary returned nil error for non-2xx response")
	}
}
