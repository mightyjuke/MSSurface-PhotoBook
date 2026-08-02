package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/disintegration/imaging"
)

const maxUploadBytes = 512 << 20

var buildVersion = "dev"

//go:embed web/*
var webFiles embed.FS

type Config struct {
	Title           string `json:"title"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Transition      string `json:"transition"`
	Fit             string `json:"fit"`
	Shuffle         bool   `json:"shuffle"`
	ShowClock       bool   `json:"showClock"`
	Background      string `json:"background"`
}

type Photo struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"originalName"`
	Size         int64     `json:"size"`
	UploadedAt   time.Time `json:"uploadedAt"`
	URL          string    `json:"url"`
	ThumbnailURL string    `json:"thumbnailUrl"`
	DisplayURL   string    `json:"displayUrl"`
}

type persistedState struct {
	Config  Config  `json:"config"`
	Photos  []Photo `json:"photos"`
	Version string  `json:"version,omitempty"`
}

type UpdateStatus struct {
	CurrentVersion string `json:"currentVersion"`
	Enabled        bool   `json:"enabled"`
	State          string `json:"state"`
	Message        string `json:"message"`
	CheckedAt      string `json:"checkedAt,omitempty"`
}

type Store struct {
	mu          sync.RWMutex
	renditionMu sync.Mutex
	dataDir     string
	mediaDir    string
	thumbDir    string
	displayDir  string
	state       persistedState
}

func defaultConfig() Config {
	return Config{
		Title:           "Our memories",
		IntervalSeconds: 12,
		Transition:      "fade",
		Fit:             "contain",
		Shuffle:         true,
		ShowClock:       false,
		Background:      "#0d0d0f",
	}
}

func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		dataDir: dataDir, mediaDir: filepath.Join(dataDir, "photos"),
		thumbDir: filepath.Join(dataDir, "thumbnails"), displayDir: filepath.Join(dataDir, "display-images"),
	}
	s.state.Config = defaultConfig()
	s.state.Photos = make([]Photo, 0)
	for _, dir := range []string{s.mediaDir, s.thumbDir, s.displayDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.state.Photos == nil {
		s.state.Photos = make([]Photo, 0)
	}
	if err := validateConfig(&s.state.Config); err != nil {
		return nil, fmt.Errorf("invalid stored config: %w", err)
	}
	return s, nil
}

func (s *Store) GetUpdateStatus() UpdateStatus {
	status := UpdateStatus{
		CurrentVersion: buildVersion,
		Enabled:        !fileExists(filepath.Join(s.dataDir, "updates-disabled")),
		State:          "idle",
		Message:        "No update check has run yet.",
	}
	b, err := os.ReadFile(filepath.Join(s.dataDir, "update-status.json"))
	if err == nil {
		var stored UpdateStatus
		if json.Unmarshal(b, &stored) == nil {
			status.State = stored.State
			status.Message = stored.Message
			status.CheckedAt = stored.CheckedAt
		}
	}
	return status
}

func (s *Store) SetAutoUpdate(enabled bool) error {
	marker := filepath.Join(s.dataDir, "updates-disabled")
	if enabled {
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(marker, []byte("Automatic updates disabled from the PhotoBook admin UI.\n"), 0640)
}

func (s *Store) RequestUpdate() error {
	status := UpdateStatus{State: "queued", Message: "Update check requested."}
	if err := s.writeUpdateStatus(status); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, "update-requested"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0640)
}

func (s *Store) writeUpdateStatus(status UpdateStatus) error {
	b, err := json.Marshal(status)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dataDir, ".update-status-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(s.dataDir, "update-status.json"))
}

func (s *Store) Snapshot() persistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.state
	out.Version = buildVersion
	out.Photos = append([]Photo{}, s.state.Photos...)
	for i := range out.Photos {
		out.Photos[i].URL = "/media/" + out.Photos[i].ID
		out.Photos[i].ThumbnailURL = "/thumbnail/" + out.Photos[i].ID
		out.Photos[i].DisplayURL = "/display-media/" + out.Photos[i].ID
	}
	return out
}

func (s *Store) UpdateConfig(c Config) error {
	if err := validateConfig(&c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Config
	s.state.Config = c
	if err := s.saveLocked(); err != nil {
		s.state.Config = previous
		return err
	}
	return nil
}

func validateConfig(c *Config) error {
	c.Title = strings.TrimSpace(c.Title)
	if len(c.Title) > 80 {
		return errors.New("title must be 80 characters or fewer")
	}
	if c.IntervalSeconds < 3 || c.IntervalSeconds > 300 {
		return errors.New("interval must be between 3 and 300 seconds")
	}
	if c.Transition != "fade" && c.Transition != "fade-zoom" && c.Transition != "slide" && c.Transition != "none" {
		return errors.New("unsupported transition")
	}
	if c.Fit != "contain" && c.Fit != "cover" {
		return errors.New("unsupported image fit")
	}
	if len(c.Background) != 7 || c.Background[0] != '#' {
		return errors.New("background must be a hex color")
	}
	for _, ch := range c.Background[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return errors.New("background must be a hex color")
		}
	}
	return nil
}

func (s *Store) AddPhoto(name, contentType string, src io.Reader) (Photo, error) {
	ext, ok := map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp",
	}[contentType]
	if !ok {
		return Photo{}, errors.New("only JPEG, PNG, GIF, and WebP images are supported")
	}
	id, err := randomID()
	if err != nil {
		return Photo{}, err
	}
	filename := id + ext
	tmp, err := os.CreateTemp(s.mediaDir, ".upload-*")
	if err != nil {
		return Photo{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, copyErr := io.Copy(tmp, io.LimitReader(src, maxUploadBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return Photo{}, copyErr
	}
	if closeErr != nil {
		return Photo{}, closeErr
	}
	if n > maxUploadBytes {
		return Photo{}, errors.New("image exceeds the 512 MB limit")
	}
	if err := os.Rename(tmpName, filepath.Join(s.mediaDir, filename)); err != nil {
		return Photo{}, err
	}
	p := Photo{
		ID: filename, OriginalName: filepath.Base(name), Size: n, UploadedAt: time.Now().UTC(),
		URL: "/media/" + filename, ThumbnailURL: "/thumbnail/" + filename, DisplayURL: "/display-media/" + filename,
	}
	// Rendition failures never reject an otherwise valid original. Formats the
	// renderer cannot decode fall back to the original in the HTTP handlers.
	_ = s.ensureRenditions(filename, filepath.Join(s.mediaDir, filename))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Photos = append(s.state.Photos, p)
	if err := s.saveLocked(); err != nil {
		s.state.Photos = s.state.Photos[:len(s.state.Photos)-1]
		_ = os.Remove(filepath.Join(s.mediaDir, filename))
		_ = os.Remove(filepath.Join(s.thumbDir, filename+".jpg"))
		_ = os.Remove(filepath.Join(s.displayDir, filename+".contain.jpg"))
		_ = os.Remove(filepath.Join(s.displayDir, filename+".cover.jpg"))
		return Photo{}, err
	}
	return p, nil
}

func (s *Store) DeletePhoto(id string) error {
	_, err := s.DeletePhotos([]string{id})
	return err
}

func (s *Store) DeletePhotos(ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	previous := s.state.Photos
	kept := make([]Photo, 0, len(previous))
	removed := make([]Photo, 0, len(ids))
	for _, photo := range previous {
		if _, ok := wanted[photo.ID]; ok {
			removed = append(removed, photo)
		} else {
			kept = append(kept, photo)
		}
	}
	if len(removed) == 0 {
		return 0, os.ErrNotExist
	}
	s.state.Photos = kept
	if err := s.saveLocked(); err != nil {
		s.state.Photos = previous
		return 0, err
	}
	for _, photo := range removed {
		if err := os.Remove(filepath.Join(s.mediaDir, photo.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return len(removed), err
		}
		_ = os.Remove(filepath.Join(s.thumbDir, photo.ID+".jpg"))
		_ = os.Remove(filepath.Join(s.displayDir, photo.ID+".contain.jpg"))
		_ = os.Remove(filepath.Join(s.displayDir, photo.ID+".cover.jpg"))
	}
	return len(removed), nil
}

func (s *Store) ReorderPhotos(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) != len(s.state.Photos) {
		return errors.New("order must contain every photo")
	}
	byID := make(map[string]Photo, len(s.state.Photos))
	for _, photo := range s.state.Photos {
		byID[photo.ID] = photo
	}
	ordered := make([]Photo, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		photo, ok := byID[id]
		if !ok {
			return errors.New("order contains an unknown photo")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("order contains a duplicate photo")
		}
		seen[id] = struct{}{}
		ordered = append(ordered, photo)
	}
	previous := s.state.Photos
	s.state.Photos = ordered
	if err := s.saveLocked(); err != nil {
		s.state.Photos = previous
		return err
	}
	return nil
}

func (s *Store) PhotoPath(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.state.Photos {
		if p.ID == id {
			return filepath.Join(s.mediaDir, p.ID), true
		}
	}
	return "", false
}

func (s *Store) ThumbnailPath(id string) (string, error) {
	source, ok := s.PhotoPath(id)
	if !ok {
		return "", os.ErrNotExist
	}
	if err := s.ensureRenditions(id, source); err != nil {
		return "", err
	}
	return filepath.Join(s.thumbDir, id+".jpg"), nil
}

func (s *Store) DisplayPath(id, fit string) (string, error) {
	if fit != "contain" && fit != "cover" {
		return "", errors.New("unsupported image fit")
	}
	source, ok := s.PhotoPath(id)
	if !ok {
		return "", os.ErrNotExist
	}
	if err := s.ensureRenditions(id, source); err != nil {
		return "", err
	}
	return filepath.Join(s.displayDir, id+"."+fit+".jpg"), nil
}

func (s *Store) ensureRenditions(id, source string) error {
	thumbnailPath := filepath.Join(s.thumbDir, id+".jpg")
	containPath := filepath.Join(s.displayDir, id+".contain.jpg")
	coverPath := filepath.Join(s.displayDir, id+".cover.jpg")
	if filesExist(thumbnailPath, containPath, coverPath) {
		return nil
	}
	s.renditionMu.Lock()
	defer s.renditionMu.Unlock()
	if filesExist(thumbnailPath, containPath, coverPath) {
		return nil
	}
	sourceImage, err := imaging.Open(source, imaging.AutoOrientation(true))
	if err != nil {
		return err
	}
	if !fileExists(thumbnailPath) {
		if err := writeJPEGAtomic(thumbnailPath, imaging.Fit(sourceImage, 720, 540, imaging.Lanczos), 82); err != nil {
			return err
		}
	}
	if !fileExists(containPath) {
		if err := writeJPEGAtomic(containPath, imaging.Fit(sourceImage, 1366, 768, imaging.Lanczos), 88); err != nil {
			return err
		}
	}
	if !fileExists(coverPath) {
		bounds := sourceImage.Bounds()
		cover := imaging.Clone(sourceImage)
		if bounds.Dx()*bounds.Dy() > 1366*768 {
			cover = imaging.Fill(sourceImage, 1366, 768, imaging.Center, imaging.Lanczos)
		}
		if err := writeJPEGAtomic(coverPath, cover, 88); err != nil {
			return err
		}
	}
	return nil
}

func filesExist(paths ...string) bool {
	for _, path := range paths {
		if !fileExists(path) {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeJPEGAtomic(target string, img image.Image, quality int) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".rendition-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := jpeg.Encode(tmp, img, &jpeg.Options{Quality: quality}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dataDir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dataDir, "state.json"))
}

func randomID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type App struct {
	store    *Store
	password string
	logger   *slog.Logger
	web      http.Handler
}

func NewApp(store *Store, password string, logger *slog.Logger) *App {
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return &App{store: store, password: password, logger: logger, web: http.FileServer(http.FS(webRoot))}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	webHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		a.web.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildVersion})
	})
	mux.HandleFunc("GET /api/frame", a.getFrame)
	mux.HandleFunc("GET /media/{id}", a.getMedia)
	mux.HandleFunc("GET /thumbnail/{id}", a.getThumbnail)
	mux.HandleFunc("GET /display-media/{id}", a.getDisplayMedia)
	mux.Handle("GET /display/", http.StripPrefix("/display/", webHandler))
	mux.Handle("GET /admin/", a.requireAdmin(http.StripPrefix("/admin/", webHandler)))
	mux.Handle("GET /api/admin/state", a.requireAdmin(http.HandlerFunc(a.getState)))
	mux.Handle("PUT /api/admin/config", a.requireAdmin(http.HandlerFunc(a.putConfig)))
	mux.Handle("POST /api/admin/photos", a.requireAdmin(http.HandlerFunc(a.postPhotos)))
	mux.Handle("DELETE /api/admin/photos", a.requireAdmin(http.HandlerFunc(a.deletePhotos)))
	mux.Handle("PUT /api/admin/photos/order", a.requireAdmin(http.HandlerFunc(a.putPhotoOrder)))
	mux.Handle("DELETE /api/admin/photos/{id}", a.requireAdmin(http.HandlerFunc(a.deletePhoto)))
	mux.Handle("GET /api/admin/update", a.requireAdmin(http.HandlerFunc(a.getUpdateStatus)))
	mux.Handle("PUT /api/admin/update", a.requireAdmin(http.HandlerFunc(a.putUpdateSettings)))
	mux.Handle("POST /api/admin/update/check", a.requireAdmin(http.HandlerFunc(a.postUpdateCheck)))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/display/", http.StatusTemporaryRedirect)
	})
	return a.securityHeaders(a.logRequests(mux))
}

func (a *App) getFrame(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Snapshot())
}
func (a *App) getState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Snapshot())
}

func (a *App) getUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.GetUpdateStatus())
}

func (a *App) putUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	var request struct {
		Enabled bool `json:"enabled"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid update setting")
		return
	}
	if err := a.store.SetAutoUpdate(request.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "could not change automatic updates")
		return
	}
	writeJSON(w, http.StatusOK, a.store.GetUpdateStatus())
}

func (a *App) postUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	if err := a.store.RequestUpdate(); err != nil {
		writeError(w, http.StatusInternalServerError, "could not request an update check")
		return
	}
	writeJSON(w, http.StatusAccepted, a.store.GetUpdateStatus())
}

func (a *App) getMedia(w http.ResponseWriter, r *http.Request) {
	path, ok := a.store.PhotoPath(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

func (a *App) getThumbnail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path, err := a.store.ThumbnailPath(id)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if _, ok := a.store.PhotoPath(id); ok {
			http.Redirect(w, r, "/media/"+id, http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

func (a *App) getDisplayMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path, err := a.store.DisplayPath(id, r.URL.Query().Get("fit"))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if _, ok := a.store.PhotoPath(id); ok {
			http.Redirect(w, r, "/media/"+id, http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

func (a *App) putConfig(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	var c Config
	dec := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings")
		return
	}
	if err := a.store.UpdateConfig(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *App) postPhotos(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}
	var added []Photo
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read upload")
			return
		}
		if part.FileName() == "" {
			_ = part.Close()
			continue
		}
		buf := make([]byte, 512)
		n, readErr := io.ReadFull(part, buf)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			_ = part.Close()
			writeError(w, http.StatusBadRequest, "could not read image")
			return
		}
		contentType := http.DetectContentType(buf[:n])
		photo, addErr := a.store.AddPhoto(part.FileName(), contentType, io.MultiReader(bytes.NewReader(buf[:n]), part))
		_ = part.Close()
		if addErr != nil {
			writeError(w, http.StatusBadRequest, part.FileName()+": "+addErr.Error())
			return
		}
		added = append(added, photo)
	}
	if len(added) == 0 {
		writeError(w, http.StatusBadRequest, "no images were selected")
		return
	}
	writeJSON(w, http.StatusCreated, added)
}

func (a *App) deletePhoto(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	if err := a.store.DeletePhoto(r.PathValue("id")); errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete photo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) deletePhotos(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	var request struct {
		IDs []string `json:"ids"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil || len(request.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "select at least one photo")
		return
	}
	if len(request.IDs) > 1000 {
		writeError(w, http.StatusBadRequest, "cannot remove more than 1000 photos at once")
		return
	}
	removed, err := a.store.DeletePhotos(request.IDs)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "selected photos were not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove every selected photo")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

func (a *App) putPhotoOrder(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid request origin")
		return
	}
	var request struct {
		IDs []string `json:"ids"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid photo order")
		return
	}
	if len(request.IDs) > 1000 {
		writeError(w, http.StatusBadRequest, "cannot arrange more than 1000 photos at once")
		return
	}
	if err := a.store.ReorderPhotos(request.IDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.store.Snapshot())
}

func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.password == "" {
			next.ServeHTTP(w, r)
			return
		}
		user, password, ok := r.BasicAuth()
		expectedUser := sha256.Sum256([]byte("admin"))
		actualUser := sha256.Sum256([]byte(user))
		expectedPassword := sha256.Sum256([]byte(a.password))
		actualPassword := sha256.Sum256([]byte(password))
		if !ok || subtle.ConstantTimeCompare(actualUser[:], expectedUser[:]) != 1 || subtle.ConstantTimeCompare(actualPassword[:], expectedPassword[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="PhotoBook admin", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "sign in as admin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/health" {
			a.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildVersion)
		return
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := envOr("PHOTOBOOK_DATA_DIR", "./data")
	address := envOr("PHOTOBOOK_ADDRESS", ":8080")
	store, err := NewStore(dataDir)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	password := os.Getenv("PHOTOBOOK_ADMIN_PASSWORD")
	if password == "" {
		logger.Warn("admin UI has no password; set PHOTOBOOK_ADMIN_PASSWORD before exposing it to a network")
	}
	server := &http.Server{Addr: address, Handler: NewApp(store, password, logger).Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		logger.Info("PhotoBook is ready", "address", address, "data", dataDir, "version", buildVersion, "go", runtime.Version())
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	_ = server.Close()
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func init() {
	mime.AddExtensionType(".webp", "image/webp")
}
