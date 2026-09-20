package strava

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withServer starts an httptest server, points tokenURL and uploadsURL at
// it (a test double for both Strava endpoints, since a single server can
// route by path), and restores the real URLs when the test finishes.
func withServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	origToken, origUploads := tokenURL, uploadsURL
	tokenURL = srv.URL + "/oauth/token"
	uploadsURL = srv.URL + "/api/v3/uploads"
	t.Cleanup(func() {
		tokenURL, uploadsURL = origToken, origUploads
	})
	return srv
}

func TestTokenRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	want := Token{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 12345}
	if err := saveToken(want); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	path, err := TokenPath()
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 0600", perm)
	}

	got, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if got != want {
		t.Errorf("loadToken() = %+v, want %+v", got, want)
	}
}

func TestLoadToken_MissingFile(t *testing.T) {
	withTempConfigDir(t)
	if _, err := loadToken(); err == nil {
		t.Fatal("loadToken() with no saved token: want error, got nil")
	}
}

func TestPostForToken_Success(t *testing.T) {
	srv := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		json.NewEncoder(w).Encode(Token{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: 999})
	})
	_ = srv

	tok, err := postForToken(map[string]string{"grant_type": "authorization_code", "code": "abc"})
	if err != nil {
		t.Fatalf("postForToken: %v", err)
	}
	if tok.AccessToken != "new-access" || tok.RefreshToken != "new-refresh" || tok.ExpiresAt != 999 {
		t.Errorf("postForToken() = %+v, unexpected", tok)
	}
}

func TestPostForToken_ErrorStatus(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"invalid client"}`)
	})

	if _, err := postForToken(map[string]string{}); err == nil {
		t.Fatal("postForToken() with 401 response: want error, got nil")
	}
}

func TestExchangeCode_Success(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		if got := r.Form.Get("code"); got != "auth-code" {
			t.Errorf("code = %q, want auth-code", got)
		}
		json.NewEncoder(w).Encode(Token{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 42})
	})

	tok, err := ExchangeCode("id", "secret", "auth-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "access" || tok.RefreshToken != "refresh" || tok.ExpiresAt != 42 {
		t.Errorf("ExchangeCode() = %+v, unexpected", tok)
	}

	// ExchangeCode must not persist anything locally - unlike the CLI's own
	// exchangeCode, callers manage their own storage (e.g. a multi-user
	// web server).
	withTempConfigDir(t)
	if _, err := loadToken(); err == nil {
		t.Error("ExchangeCode() persisted a token locally; it should not")
	}
}

func TestExchangeCode_ErrorStatus(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"invalid code"}`)
	})

	if _, err := ExchangeCode("id", "secret", "bad-code"); err == nil {
		t.Fatal("ExchangeCode() with a 400 response: want error, got nil")
	}
}

func TestRefreshAccessToken_Success(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", got)
		}
		json.NewEncoder(w).Encode(Token{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: 99})
	})

	tok, err := RefreshAccessToken("id", "secret", "old-refresh")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if tok.AccessToken != "new-access" {
		t.Errorf("RefreshAccessToken() = %+v, unexpected", tok)
	}
}

func TestAccessToken_ReturnsCachedTokenWhenNotExpired(t *testing.T) {
	withTempConfigDir(t)
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("token endpoint should not be called when the cached token is still valid")
	})

	want := Token{AccessToken: "still-valid", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := saveToken(want); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	got, err := AccessToken("id", "secret")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "still-valid" {
		t.Errorf("AccessToken() = %q, want still-valid", got)
	}
}

func TestAccessToken_RefreshesExpiredToken(t *testing.T) {
	withTempConfigDir(t)
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("refresh_token = %q, want old-refresh", got)
		}
		json.NewEncoder(w).Encode(Token{AccessToken: "refreshed-access", RefreshToken: "new-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	})

	expired := Token{AccessToken: "expired", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	if err := saveToken(expired); err != nil {
		t.Fatalf("saveToken: %v", err)
	}

	got, err := AccessToken("id", "secret")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "refreshed-access" {
		t.Errorf("AccessToken() = %q, want refreshed-access", got)
	}

	// The refreshed token should have been persisted for next time.
	saved, err := loadToken()
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if saved.AccessToken != "refreshed-access" {
		t.Errorf("persisted AccessToken = %q, want refreshed-access", saved.AccessToken)
	}
}

func TestUploadTCX_Success(t *testing.T) {
	tcxPath := filepath.Join(t.TempDir(), "workout.tcx")
	if err := os.WriteFile(tcxPath, []byte("<TrainingCenterDatabase/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm: %v", err)
			}
			if got := r.FormValue("data_type"); got != "tcx" {
				t.Errorf("data_type = %q, want tcx", got)
			}
			if got := r.FormValue("activity_type"); got != "rowing" {
				t.Errorf("activity_type = %q, want rowing", got)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"id": 555})
		case r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"activity_id": 999})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	result, err := UploadTCX("test-token", tcxPath, "My Workout", "a description", "rowing")
	if err != nil {
		t.Fatalf("UploadTCX: %v", err)
	}
	if result.ActivityID != 999 {
		t.Errorf("ActivityID = %d, want 999", result.ActivityID)
	}
}

func TestUploadTCX_ProcessingError(t *testing.T) {
	tcxPath := filepath.Join(t.TempDir(), "workout.tcx")
	if err := os.WriteFile(tcxPath, []byte("<TrainingCenterDatabase/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"id": 1})
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"error": "duplicate activity"})
		}
	})

	_, err := UploadTCX("test-token", tcxPath, "name", "", "workout")
	if err == nil {
		t.Fatal("UploadTCX() with a processing error: want error, got nil")
	}
}

func TestUploadTCX_UploadRejected(t *testing.T) {
	tcxPath := filepath.Join(t.TempDir(), "workout.tcx")
	if err := os.WriteFile(tcxPath, []byte("<TrainingCenterDatabase/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"file is not a valid TCX file"}`)
	})

	if _, err := UploadTCX("test-token", tcxPath, "name", "", "workout"); err == nil {
		t.Fatal("UploadTCX() with a rejected upload: want error, got nil")
	}
}

// TestPollUpload_NonOKStatus covers a bug where a non-200 response while
// polling upload status (e.g. an expired token, or a 5xx from Strava) was
// silently ignored - the response body was decoded as if it were a normal
// (empty) status, so pollUpload just kept looping every 2s until its
// 2-minute deadline, masking the real error behind a generic timeout.
// pollUpload must now return immediately on a non-200 status.
func TestPollUpload_NonOKStatus(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Authorization Error"}`)
	})

	start := time.Now()
	_, err := pollUpload("test-token", 42)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("pollUpload() with a 401 response: want error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("pollUpload() took %v to return an error; it should fail immediately rather than looping until its timeout", elapsed)
	}
}
