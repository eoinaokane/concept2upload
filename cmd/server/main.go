// Command server is the multi-user web app counterpart to
// cmd/concept2upload: it exposes the same list/get workout operations as a
// small JSON API, backed by Firebase Auth (who's asking) and Firestore
// (each user's Concept2 token), so it can serve many people at once
// instead of one local CLI user. Uploading to Strava stays a CLI-only
// feature (cmd/concept2upload's upload-strava) - Strava now gates
// registering an API application behind a paid plan, so the web app
// doesn't attempt its own Strava OAuth flow.
//
// It's designed to run on Cloud Run behind Firebase Hosting (see
// firebase.json and Dockerfile at the repo root): Hosting terminates the
// public domain and forwards /api/** here, Cloud Run's default service
// account gives this process credentials for Firebase/Firestore with no
// key file needed, and the frontend in web/ talks to this API directly.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	fbauth "firebase.google.com/go/v4/auth"

	"github.com/eoinaokane/concept2upload/internal/concept2"
	"github.com/eoinaokane/concept2upload/internal/tcx"
	"github.com/eoinaokane/concept2upload/internal/webstore"
)

func main() {
	ctx := context.Background()

	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		log.Fatalf("initializing Firebase app: %v", err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		log.Fatalf("initializing Firebase Auth client: %v", err)
	}
	fsClient, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("initializing Firestore client: %v", err)
	}
	defer fsClient.Close()

	srv := &server{
		auth:  authClient,
		store: webstore.New(fsClient),
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/concept2-token", srv.withAuth(srv.handleSaveConcept2Token))
	mux.Handle("GET /api/workouts", srv.withAuth(srv.handleListWorkouts))
	mux.Handle("GET /api/workouts/{id}", srv.withAuth(srv.handleGetWorkout))
	mux.Handle("GET /api/workouts/{id}/tcx", srv.withAuth(srv.handleGetWorkoutTCX))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

// idTokenVerifier is the subset of *auth.Client (Firebase Admin SDK) this
// server depends on, so tests can substitute a fake verifier instead of
// needing a real Firebase project.
type idTokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*fbauth.Token, error)
}

// tokenStore is the subset of *webstore.Store this server depends on, so
// tests can substitute an in-memory fake instead of needing a real
// Firestore project. *webstore.Store already satisfies this.
type tokenStore interface {
	GetConcept2Token(ctx context.Context, uid string) (string, error)
	SaveConcept2Token(ctx context.Context, uid, token string) error
}

type server struct {
	auth  idTokenVerifier
	store tokenStore
}

// --- auth middleware ---------------------------------------------------

type ctxKey int

const uidKey ctxKey = 0

// withAuth requires a Firebase Auth ID token in the Authorization header
// (as sent by the Firebase JS SDK after sign-in), verifies it, and makes
// the resulting UID available to the handler via uidFromContext.
func (s *server) withAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		idToken, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || idToken == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		token, err := s.auth.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), uidKey, token.UID)
		next(w, r.WithContext(ctx))
	})
}

func uidFromContext(ctx context.Context) string {
	uid, _ := ctx.Value(uidKey).(string)
	return uid
}

// withCORS allows the frontend (served from Firebase Hosting, possibly on
// a different origin during local development) to call this API directly.
// In production behind Firebase Hosting rewrites, frontend and API share
// one origin and this is a no-op.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// concept2Client resolves uid's saved Concept2 token and returns a client,
// or a user-facing error if they haven't run "save token" yet.
func (s *server) concept2Client(r *http.Request) (*concept2.Client, error) {
	uid := uidFromContext(r.Context())
	token, err := s.store.GetConcept2Token(r.Context(), uid)
	if errors.Is(err, webstore.ErrNotFound) {
		return nil, fmt.Errorf("no Concept2 token saved; POST /api/concept2-token first")
	}
	if err != nil {
		return nil, err
	}
	return concept2.NewClient(token), nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid workout id")
	}
	return id, nil
}

// --- handlers --------------------------------------------------------------

func (s *server) handleSaveConcept2Token(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" {
		writeError(w, http.StatusBadRequest, "expected JSON body {\"token\": \"...\"}")
		return
	}
	uid := uidFromContext(r.Context())
	if err := s.store.SaveConcept2Token(r.Context(), uid, strings.TrimSpace(body.Token)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// workoutSummary is the JSON shape for one row of "list" - unlike the
// CLI's positional numbering (backed by a .last_list.json cache file),
// the web API just returns each workout's real Concept2 result ID, which
// the frontend keeps client-side and uses directly in later requests.
type workoutSummary struct {
	ID            int64  `json:"id"`
	Date          string `json:"date"`
	Timezone      string `json:"timezone,omitempty"` // IANA name where the workout was recorded, e.g. "Europe/Dublin" - not the viewer's own timezone
	Type          string `json:"type"`
	Distance      int    `json:"distanceMetres"`
	TimeFormatted string `json:"timeFormatted"`
	WorkoutType   string `json:"workoutType"`
}

func (s *server) handleListWorkouts(w http.ResponseWriter, r *http.Request) {
	client, err := s.concept2Client(r)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	results, err := client.ListLatest(limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, "listing Concept2 workouts: "+err.Error())
		return
	}
	if len(results) > limit {
		results = results[:limit]
	}

	summaries := make([]workoutSummary, 0, len(results))
	for _, res := range results {
		dateStr := res.Date
		if start, err := res.StartTime(); err == nil {
			dateStr = start.Format(time.RFC3339)
		}
		summaries = append(summaries, workoutSummary{
			ID:            res.ID,
			Date:          dateStr,
			Timezone:      res.Timezone,
			Type:          res.Type,
			Distance:      res.Distance,
			TimeFormatted: res.TimeFormatted,
			WorkoutType:   res.WorkoutType,
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

// workoutDetail is the JSON shape of "show", covering the same fields the
// CLI's printWorkoutMetadata prints.
type workoutDetail struct {
	workoutSummary
	Calories      int    `json:"calories"`
	DragFactor    int    `json:"dragFactor,omitempty"`
	StrokeRate    int    `json:"strokeRate,omitempty"`
	AvgWatts      int    `json:"avgWatts,omitempty"`
	HeartRateAvg  int    `json:"heartRateAvg,omitempty"`
	HeartRateMax  int    `json:"heartRateMax,omitempty"`
	Source        string `json:"source,omitempty"`
	HasStrokeData bool   `json:"hasStrokeData"`
	Comments      string `json:"comments,omitempty"`
}

func (s *server) fetchDetail(r *http.Request) (concept2.ResultDetail, error) {
	client, err := s.concept2Client(r)
	if err != nil {
		return concept2.ResultDetail{}, err
	}
	id, err := pathID(r)
	if err != nil {
		return concept2.ResultDetail{}, err
	}
	return client.GetResultDetail(id)
}

func (s *server) handleGetWorkout(w http.ResponseWriter, r *http.Request) {
	detail, err := s.fetchDetail(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	dateStr := detail.Date
	if start, err := detail.StartTime(); err == nil {
		dateStr = start.Format(time.RFC3339)
	}
	watts := tcx.WattsFromDistanceTime(detail.Distance, detail.Time, tcx.SplitDistanceMetres(detail.Type))

	writeJSON(w, http.StatusOK, workoutDetail{
		workoutSummary: workoutSummary{
			ID:            detail.ID,
			Date:          dateStr,
			Timezone:      detail.Timezone,
			Type:          detail.Type,
			Distance:      detail.Distance,
			TimeFormatted: detail.TimeFormatted,
			WorkoutType:   detail.WorkoutType,
		},
		Calories:      detail.CaloriesTotal,
		DragFactor:    detail.DragFactor,
		StrokeRate:    detail.StrokeRate,
		AvgWatts:      watts,
		HeartRateAvg:  detail.HeartRate.Average,
		HeartRateMax:  detail.HeartRate.Max,
		Source:        detail.Source,
		HasStrokeData: detail.StrokeData,
		Comments:      detail.Comments,
	})
}

func (s *server) handleGetWorkoutTCX(w http.ResponseWriter, r *http.Request) {
	detail, err := s.fetchDetail(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	body, err := tcx.Build(detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building TCX: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.garmin.tcx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="workout-%d.tcx"`, detail.ID))
	_, _ = w.Write(body)
}
