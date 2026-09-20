package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fbauth "firebase.google.com/go/v4/auth"

	"github.com/eoinaokane/concept2upload/internal/webstore"
)

// fakeVerifier is a test double for idTokenVerifier: idToken values are
// looked up directly against a map, standing in for what a real Firebase
// project would otherwise need to verify a JWT.
type fakeVerifier struct {
	uidByToken map[string]string
}

func (f *fakeVerifier) VerifyIDToken(ctx context.Context, idToken string) (*fbauth.Token, error) {
	uid, ok := f.uidByToken[idToken]
	if !ok {
		return nil, errors.New("invalid token")
	}
	return &fbauth.Token{UID: uid}, nil
}

// fakeStore is an in-memory test double for tokenStore, standing in for
// what a real Firestore project would otherwise need.
type fakeStore struct {
	concept2Tokens map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{concept2Tokens: map[string]string{}}
}

func (f *fakeStore) GetConcept2Token(ctx context.Context, uid string) (string, error) {
	tok, ok := f.concept2Tokens[uid]
	if !ok {
		return "", webstore.ErrNotFound
	}
	return tok, nil
}

func (f *fakeStore) SaveConcept2Token(ctx context.Context, uid, token string) error {
	f.concept2Tokens[uid] = token
	return nil
}

func (f *fakeStore) DeleteConcept2Token(ctx context.Context, uid string) error {
	delete(f.concept2Tokens, uid)
	return nil
}

func withUID(r *http.Request, uid string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), uidKey, uid))
}

// --- auth middleware ---------------------------------------------------

func TestWithAuth(t *testing.T) {
	srv := &server{auth: &fakeVerifier{uidByToken: map[string]string{"good-token": "uid-1"}}}

	var gotUID string
	handler := srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
		gotUID = uidFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	t.Run("missing header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		gotUID = ""
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if gotUID != "uid-1" {
			t.Errorf("uid reaching the handler = %q, want uid-1", gotUID)
		}
	})
}

// --- handleSaveConcept2Token ---------------------------------------------

func TestHandleSaveConcept2Token(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := newFakeStore()
		srv := &server{store: store}

		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`{"token":"c2-token"}`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if store.concept2Tokens["uid-1"] != "c2-token" {
			t.Errorf("stored token = %q, want c2-token", store.concept2Tokens["uid-1"])
		}
	})

	t.Run("empty token", func(t *testing.T) {
		srv := &server{store: newFakeStore()}
		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`{}`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		srv := &server{store: newFakeStore()}
		req := withUID(httptest.NewRequest(http.MethodPost, "/api/concept2-token", strings.NewReader(`not json`)), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleSaveConcept2Token(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestHandleDeleteConcept2Token(t *testing.T) {
	store := newFakeStore()
	store.concept2Tokens["uid-1"] = "c2-token"
	srv := &server{store: store}

	req := withUID(httptest.NewRequest(http.MethodDelete, "/api/concept2-token", nil), "uid-1")
	rec := httptest.NewRecorder()
	srv.handleDeleteConcept2Token(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
	}
	if _, ok := store.concept2Tokens["uid-1"]; ok {
		t.Error("token still present after delete")
	}
}

func TestHandleGetConcept2TokenStatus(t *testing.T) {
	decodeSaved := func(t *testing.T, rec *httptest.ResponseRecorder) bool {
		t.Helper()
		var body struct {
			Saved bool `json:"saved"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		return body.Saved
	}

	t.Run("token saved", func(t *testing.T) {
		store := newFakeStore()
		store.concept2Tokens["uid-1"] = "c2-token"
		srv := &server{store: store}

		req := withUID(httptest.NewRequest(http.MethodGet, "/api/concept2-token", nil), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleGetConcept2TokenStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if !decodeSaved(t, rec) {
			t.Error("saved = false, want true")
		}
	})

	t.Run("no token saved", func(t *testing.T) {
		srv := &server{store: newFakeStore()}

		req := withUID(httptest.NewRequest(http.MethodGet, "/api/concept2-token", nil), "uid-1")
		rec := httptest.NewRecorder()
		srv.handleGetConcept2TokenStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body)
		}
		if decodeSaved(t, rec) {
			t.Error("saved = true, want false")
		}
	})
}

// --- handleListWorkouts / handleGetWorkout / handleGetWorkoutTCX
// (precondition-failure path only - the success path needs the real
// Concept2 API, which internal/concept2's baseURL const doesn't let tests
// point elsewhere) --------------------------------------------------------

func TestHandleListWorkouts_NoConcept2Token(t *testing.T) {
	srv := &server{store: newFakeStore()}
	req := withUID(httptest.NewRequest(http.MethodGet, "/api/workouts", nil), "uid-1")
	rec := httptest.NewRecorder()
	srv.handleListWorkouts(rec, req)

	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412; body = %s", rec.Code, rec.Body)
	}
}

func TestHandleGetWorkout_NoConcept2Token(t *testing.T) {
	srv := &server{store: newFakeStore()}
	req := withUID(httptest.NewRequest(http.MethodGet, "/api/workouts/42", nil), "uid-1")
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()
	srv.handleGetWorkout(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502; body = %s", rec.Code, rec.Body)
	}
}

// --- small pure helpers ----------------------------------------------------

func TestPathID(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
		want    int64
	}{
		{"valid", "42", false, 42},
		{"zero", "0", true, 0},
		{"negative", "-1", true, 0},
		{"not a number", "abc", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetPathValue("id", tc.value)
			got, err := pathID(req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pathID(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("pathID(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}
