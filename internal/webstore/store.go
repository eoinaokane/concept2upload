// Package webstore is the multi-user counterpart to internal/concept2's
// local-file token storage: instead of one token cached on disk for
// whoever runs the CLI, it keeps one Firestore document per signed-in web
// app user, keyed by their Firebase Auth UID.
package webstore

import (
	"context"
	"errors"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNotFound is returned when a user has no saved token yet (hasn't
// called POST /api/concept2-token).
var ErrNotFound = errors.New("webstore: not found")

const usersCollection = "users"

type Store struct {
	client *firestore.Client
}

func New(client *firestore.Client) *Store {
	return &Store{client: client}
}

// userDoc mirrors, per-user, what the CLI keeps in concept2.TokenPath():
// the Concept2 API token.
type userDoc struct {
	Concept2Token string `firestore:"concept2Token,omitempty"`
}

func (s *Store) userRef(uid string) *firestore.DocumentRef {
	return s.client.Collection(usersCollection).Doc(uid)
}

// GetConcept2Token returns uid's saved Concept2 API token, or ErrNotFound.
func (s *Store) GetConcept2Token(ctx context.Context, uid string) (string, error) {
	snap, err := s.userRef(uid).Get(ctx)
	if isNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	var d userDoc
	if err := snap.DataTo(&d); err != nil {
		return "", err
	}
	if d.Concept2Token == "" {
		return "", ErrNotFound
	}
	return d.Concept2Token, nil
}

// SaveConcept2Token saves uid's Concept2 API token, equivalent to the CLI's
// 'auth-concept2 <token>'.
func (s *Store) SaveConcept2Token(ctx context.Context, uid, token string) error {
	_, err := s.userRef(uid).Set(ctx, map[string]interface{}{
		"concept2Token": token,
	}, firestore.MergeAll)
	return err
}

// isNotFound reports whether err is what firestore.DocumentRef.Get returns
// for a document that doesn't exist (a gRPC status error with code
// codes.NotFound) - the client library has no plain sentinel for this.
func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}
