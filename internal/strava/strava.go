// Package strava implements just enough of the Strava API v3
// (https://developers.strava.com/docs/reference/) to authorize a
// read+upload OAuth token and upload a TCX file as a new activity.
package strava

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	authorizeURL = "https://www.strava.com/oauth/authorize"
	redirectAddr = "127.0.0.1:8721"
	redirectPath = "/callback"
)

// tokenURL and uploadsURL are vars (rather than consts) so tests can point
// them at an httptest server.
var (
	tokenURL   = "https://www.strava.com/oauth/token"
	uploadsURL = "https://www.strava.com/api/v3/uploads"
)

// userConfigDir is os.UserConfigDir, indirected so tests can override it.
// os.UserConfigDir ignores $XDG_CONFIG_HOME on darwin (it always returns
// $HOME/Library/Application Support there), so t.Setenv("XDG_CONFIG_HOME",
// ...) alone does not isolate tests on macOS - it would otherwise read and
// write a real user's actual saved Strava credentials during `go test`.
var userConfigDir = os.UserConfigDir

// Token is the OAuth token set persisted between runs.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // unix seconds
}

// TokenPath returns the file used to persist the Strava OAuth token,
// defaulting to $XDG_CONFIG_HOME/concept2upload/strava_token.json (or the
// platform equivalent via os.UserConfigDir).
func TokenPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "concept2upload", "strava_token.json"), nil
}

func loadToken() (Token, error) {
	path, err := TokenPath()
	if err != nil {
		return Token{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Token{}, err
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return Token{}, err
	}
	return t, nil
}

func saveToken(t Token) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

// Config holds the Strava API application's Client ID/Secret (from
// https://www.strava.com/settings/api), persisted so they only need to be
// supplied once via --client-id/--client-secret or
// STRAVA_CLIENT_ID/STRAVA_CLIENT_SECRET.
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// ConfigPath returns the file used to persist Config, defaulting to
// $XDG_CONFIG_HOME/concept2upload/strava.cfg (or the platform equivalent
// via os.UserConfigDir).
func ConfigPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "concept2upload", "strava.cfg"), nil
}

// LoadConfig reads a previously saved Config, if any.
func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(body, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// SaveConfig persists c to ConfigPath with owner-only permissions.
func SaveConfig(c Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

// Authorize runs the OAuth authorization-code flow: it prints (and tries to
// open) the Strava consent URL, listens on localhost for the redirect, and
// exchanges the returned code for an access/refresh token pair, which it
// persists to TokenPath(). The user authenticates and grants access in
// their own browser; this process never sees their Strava password.
func Authorize(ctx context.Context, clientID, clientSecret string) error {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(redirectPath, func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			fmt.Fprintf(w, "Authorization denied (%s). You can close this tab.", errParam)
			errCh <- fmt.Errorf("strava: authorization denied: %s", errParam)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "Strava authorized. You can close this tab and return to the terminal.")
		codeCh <- code
	})

	server := &http.Server{Addr: redirectAddr, Handler: mux}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", redirectAddr)
	if err != nil {
		return fmt.Errorf("strava: could not start local callback server on %s: %w", redirectAddr, err)
	}
	go server.Serve(ln)
	defer server.Close()

	authURL := fmt.Sprintf(
		"%s?client_id=%s&response_type=code&redirect_uri=http://%s%s&approval_prompt=auto&scope=activity:write,read",
		authorizeURL, clientID, redirectAddr, redirectPath,
	)
	fmt.Println("Open this URL in your browser and authorize the app with your own Strava login:")
	fmt.Println(authURL)
	tryOpenBrowser(authURL)

	select {
	case code := <-codeCh:
		return exchangeCode(clientID, clientSecret, code)
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("strava: timed out waiting for authorization")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func tryOpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

func exchangeCode(clientID, clientSecret, code string) error {
	tok, err := ExchangeCode(clientID, clientSecret, code)
	if err != nil {
		return err
	}
	return saveToken(tok)
}

// ExchangeCode exchanges an OAuth authorization code for a token pair,
// without persisting it anywhere. Callers that manage their own per-user
// storage (e.g. a multi-user web server backed by a database, rather than
// this package's single-user local file) call this directly and save the
// result themselves; the CLI's own exchangeCode wraps this and saves to
// TokenPath().
func ExchangeCode(clientID, clientSecret, code string) (Token, error) {
	form := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
		"grant_type":    "authorization_code",
	}
	return postForToken(form)
}

// RefreshAccessToken exchanges a refresh token for a new token pair,
// without persisting it anywhere - see ExchangeCode.
func RefreshAccessToken(clientID, clientSecret, refreshToken string) (Token, error) {
	form := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"refresh_token": refreshToken,
		"grant_type":    "refresh_token",
	}
	return postForToken(form)
}

// AccessToken returns a valid access token, transparently refreshing it via
// the stored refresh token when it has expired.
func AccessToken(clientID, clientSecret string) (string, error) {
	tok, err := loadToken()
	if err != nil {
		return "", fmt.Errorf("strava: no saved token (%w) - run the authorize step first", err)
	}
	if time.Now().Unix() < tok.ExpiresAt-60 {
		return tok.AccessToken, nil
	}

	refreshed, err := RefreshAccessToken(clientID, clientSecret, tok.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := saveToken(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func postForToken(form map[string]string) (Token, error) {
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("strava: token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("strava: token endpoint returned %s: %s", resp.Status, string(respBody))
	}

	var t Token
	if err := json.Unmarshal(respBody, &t); err != nil {
		return Token{}, fmt.Errorf("strava: decoding token response failed: %w", err)
	}
	return t, nil
}

// UploadResult is the terminal state of a Strava upload once processing
// completes.
type UploadResult struct {
	ActivityID int64
	Error      string
}

// UploadTCX uploads a TCX file as a new Strava activity and polls until
// Strava finishes processing it, returning the resulting activity ID.
func UploadTCX(accessToken, filePath, name, description, activityType string) (UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return UploadResult{}, err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return UploadResult{}, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return UploadResult{}, err
	}
	_ = w.WriteField("data_type", "tcx")
	if name != "" {
		_ = w.WriteField("name", name)
	}
	if description != "" {
		_ = w.WriteField("description", description)
	}
	if activityType != "" {
		_ = w.WriteField("activity_type", activityType)
	}
	if err := w.Close(); err != nil {
		return UploadResult{}, err
	}

	req, err := http.NewRequest(http.MethodPost, uploadsURL, &buf)
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UploadResult{}, fmt.Errorf("strava: upload request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResult{}, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return UploadResult{}, fmt.Errorf("strava: upload returned %s: %s", resp.Status, string(body))
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return UploadResult{}, fmt.Errorf("strava: decoding upload response failed: %w", err)
	}

	return pollUpload(accessToken, created.ID)
}

func pollUpload(accessToken string, uploadID int64) (UploadResult, error) {
	url := fmt.Sprintf("%s/%d", uploadsURL, uploadID)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return UploadResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return UploadResult{}, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return UploadResult{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return UploadResult{}, fmt.Errorf("strava: upload status check returned %s: %s", resp.Status, string(body))
		}

		var status struct {
			ActivityID int64  `json:"activity_id"`
			Error      string `json:"error"`
			Status     string `json:"status"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			return UploadResult{}, fmt.Errorf("strava: decoding upload status failed: %w", err)
		}
		if status.Error != "" {
			return UploadResult{Error: status.Error}, fmt.Errorf("strava: upload failed: %s", status.Error)
		}
		if status.ActivityID != 0 {
			return UploadResult{ActivityID: status.ActivityID}, nil
		}
		time.Sleep(2 * time.Second)
	}
	return UploadResult{}, fmt.Errorf("strava: timed out waiting for upload %d to process", uploadID)
}
