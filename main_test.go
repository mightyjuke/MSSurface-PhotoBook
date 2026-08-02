package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testApp(t *testing.T, password string) (*Store, http.Handler) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return store, NewApp(store, password, logger).Handler()
}

func TestFrameStartsEmpty(t *testing.T) {
	_, handler := testApp(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/frame", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"photos":[]`)) {
		t.Fatalf("empty photos must be encoded as an array; body = %s", res.Body.String())
	}
	var state persistedState
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if len(state.Photos) != 0 || state.Config.IntervalSeconds != 12 {
		t.Fatalf("unexpected initial state: %+v", state)
	}
	if state.Version != buildVersion {
		t.Fatalf("version = %q, want %q", state.Version, buildVersion)
	}
}

func TestAdminRequiresPassword(t *testing.T) {
	_, handler := testApp(t, "secret")
	for _, tc := range []struct {
		name     string
		password string
		want     int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "nope", http.StatusUnauthorized},
		{"valid", "secret", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/state", nil)
			if tc.password != "" {
				req.SetBasicAuth("admin", tc.password)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d, want %d", res.Code, tc.want)
			}
		})
	}
}

func TestWebAssetsRequireRevalidation(t *testing.T) {
	_, handler := testApp(t, "")
	req := httptest.NewRequest(http.MethodGet, "/admin/styles.css", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	for _, origin := range []string{"https://api.open-meteo.com", "https://geocoding-api.open-meteo.com"} {
		if got := res.Header().Get("Content-Security-Policy"); !bytes.Contains([]byte(got), []byte(origin)) {
			t.Fatalf("Content-Security-Policy does not allow weather origin %q: %s", origin, got)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/display/", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("display status = %d, want 200", res.Code)
	}
	for _, className := range []string{"frame-time", "frame-date", "frame-weather"} {
		if !bytes.Contains(res.Body.Bytes(), []byte(`class="`+className)) {
			t.Fatalf("display does not include %s markup", className)
		}
	}
}

func TestAdminUpdateControls(t *testing.T) {
	store, handler := testApp(t, "")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/update", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("initial status = %d; body = %s", res.Code, res.Body.String())
	}
	var status UpdateStatus
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.CurrentVersion != buildVersion {
		t.Fatalf("unexpected initial update status: %+v", status)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/update", bytes.NewBufferString(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("disable status = %d; body = %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(filepath.Join(store.dataDir, "updates-disabled")); err != nil {
		t.Fatalf("disabled marker: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/update/check", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("check status = %d; body = %s", res.Code, res.Body.String())
	}
	status = UpdateStatus{}
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.State != "queued" || status.Enabled {
		t.Fatalf("unexpected queued update status: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(store.dataDir, "update-requested")); err != nil {
		t.Fatalf("request marker: %v", err)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/update", bytes.NewBufferString(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("enable status = %d; body = %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(filepath.Join(store.dataDir, "updates-disabled")); !os.IsNotExist(err) {
		t.Fatalf("disabled marker remained: %v", err)
	}
}

func TestUploadAndDeletePhoto(t *testing.T) {
	store, handler := testApp(t, "")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("photos", "memory.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/photos", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("upload status = %d; body = %s", res.Code, res.Body.String())
	}
	state := store.Snapshot()
	if len(state.Photos) != 1 || state.Photos[0].OriginalName != "memory.png" {
		t.Fatalf("unexpected photos: %+v", state.Photos)
	}
	if state.Photos[0].ThumbnailURL == "" {
		t.Fatal("uploaded photo has no thumbnail URL")
	}
	if state.Photos[0].DisplayURL == "" {
		t.Fatal("uploaded photo has no display URL")
	}
	req = httptest.NewRequest(http.MethodGet, state.Photos[0].ThumbnailURL, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail status = %d, content type = %q", res.Code, res.Header().Get("Content-Type"))
	}
	for _, fit := range []string{"contain", "cover"} {
		req = httptest.NewRequest(http.MethodGet, state.Photos[0].DisplayURL+"?fit="+fit, nil)
		res = httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("%s display image status = %d, content type = %q", fit, res.Code, res.Header().Get("Content-Type"))
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(res.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if config.Width > 1366 || config.Height > 768 {
			t.Fatalf("%s display image is too large: %dx%d", fit, config.Width, config.Height)
		}
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/photos/"+state.Photos[0].ID, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d; body = %s", res.Code, res.Body.String())
	}
	if len(store.Snapshot().Photos) != 0 {
		t.Fatal("photo remained after deletion")
	}
	req = httptest.NewRequest(http.MethodGet, state.Photos[0].ThumbnailURL, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("deleted thumbnail status = %d, want 404", res.Code)
	}
	req = httptest.NewRequest(http.MethodGet, state.Photos[0].DisplayURL+"?fit=contain", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("deleted display image status = %d, want 404", res.Code)
	}
}

func TestConfigValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Title = "Family archive"
	cfg.IntervalSeconds = 25
	cfg.Transition = "fade-zoom"
	if err := store.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot().Config; got.Title != cfg.Title || got.IntervalSeconds != 25 || got.Transition != "fade-zoom" {
		t.Fatalf("config was not persisted: %+v", got)
	}
	cfg.IntervalSeconds = 1
	if err := store.UpdateConfig(cfg); err == nil {
		t.Fatal("expected invalid interval to fail")
	}
}

func TestBulkDeletePhotos(t *testing.T) {
	store, handler := testApp(t, "")
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	first, err := store.AddPhoto("first.png", "image/png", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AddPhoto("second.png", "image/png", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string][]string{"ids": {first.ID, second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/photos", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", res.Code, res.Body.String())
	}
	if len(store.Snapshot().Photos) != 0 {
		t.Fatal("photos remained after bulk deletion")
	}
}

func TestReorderPhotos(t *testing.T) {
	store, handler := testApp(t, "")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var photos []Photo
	for _, name := range []string{"first.png", "second.png", "third.png"} {
		photo, err := store.AddPhoto(name, "image/png", bytes.NewReader(encoded.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		photos = append(photos, photo)
	}
	payload, err := json.Marshal(map[string][]string{"ids": {photos[2].ID, photos[0].ID, photos[1].ID}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/photos/order", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", res.Code, res.Body.String())
	}
	got := store.Snapshot().Photos
	if got[0].ID != photos[2].ID || got[1].ID != photos[0].ID || got[2].ID != photos[1].ID {
		t.Fatalf("unexpected order: %+v", got)
	}

	invalid, _ := json.Marshal(map[string][]string{"ids": {photos[0].ID, photos[0].ID, photos[1].ID}})
	req = httptest.NewRequest(http.MethodPut, "/api/admin/photos/order", bytes.NewReader(invalid))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("duplicate order status = %d, want 400", res.Code)
	}
}
