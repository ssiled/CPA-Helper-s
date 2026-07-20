package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDetectRepoRootFromPrefersProjectAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd := filepath.Join(root, "backend", "bin")
	executablePath := filepath.Join(cwd, "cpa-helper.exe")
	got, err := detectRepoRootFrom(cwd, executablePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("repo root = %q, want %q", got, root)
	}
}

func TestDetectRepoRootFromFallsBackToExecutableDir(t *testing.T) {
	cwd := t.TempDir()
	releaseDir := t.TempDir()
	executablePath := filepath.Join(releaseDir, "cpa-helper.exe")

	got, err := detectRepoRootFrom(cwd, executablePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != releaseDir {
		t.Fatalf("repo root = %q, want %q", got, releaseDir)
	}
}

func TestHandleSPAServesEmbeddedFrontendAsset(t *testing.T) {
	app := &App{frontendFS: fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>embedded</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('embedded')")},
	}}

	req := httptest.NewRequest("GET", "http://example.com/assets/app.js", nil)
	recorder := httptest.NewRecorder()
	if err := app.handleSPA(recorder, req); err != nil {
		t.Fatal(err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "console.log('embedded')") {
		t.Fatalf("body = %q", body)
	}
}

func TestHandleSPAServesSVGWithBrowserCompatibleContentType(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"></svg>`

	t.Run("embedded", func(t *testing.T) {
		app := &App{frontendFS: fstest.MapFS{
			"index.html":         &fstest.MapFile{Data: []byte("<html></html>")},
			"providers/logo.svg": &fstest.MapFile{Data: []byte(svg)},
		}}
		recorder := httptest.NewRecorder()
		if err := app.handleSPA(recorder, httptest.NewRequest("GET", "http://example.com/providers/logo.svg", nil)); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("Content-Type"); got != "image/svg+xml" {
			t.Fatalf("Content-Type = %q, want image/svg+xml", got)
		}
	})

	t.Run("external", func(t *testing.T) {
		distDir := t.TempDir()
		providerDir := filepath.Join(distDir, "providers")
		if err := os.MkdirAll(providerDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(providerDir, "logo.svg"), []byte(svg), 0o644); err != nil {
			t.Fatal(err)
		}
		app := &App{frontendDist: distDir, frontendEnv: true}
		recorder := httptest.NewRecorder()
		if err := app.handleSPA(recorder, httptest.NewRequest("GET", "http://example.com/providers/logo.svg", nil)); err != nil {
			t.Fatal(err)
		}
		if got := recorder.Header().Get("Content-Type"); got != "image/svg+xml" {
			t.Fatalf("Content-Type = %q, want image/svg+xml", got)
		}
	})
}

func TestHandleSPAFallsBackToEmbeddedIndex(t *testing.T) {
	app := &App{frontendFS: fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>embedded index</html>")},
	}}

	req := httptest.NewRequest("GET", "http://example.com/settings/account", nil)
	recorder := httptest.NewRecorder()
	if err := app.handleSPA(recorder, req); err != nil {
		t.Fatal(err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "embedded index") {
		t.Fatalf("body = %q", body)
	}
}

func TestHandleSPAFrontendDistOverrideUsesExternalFiles(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>external</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{
		frontendDist: distDir,
		frontendEnv:  true,
		frontendFS: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html>embedded</html>")},
		},
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	recorder := httptest.NewRecorder()
	if err := app.handleSPA(recorder, req); err != nil {
		t.Fatal(err)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "external") || strings.Contains(body, "embedded") {
		t.Fatalf("body = %q", body)
	}
}
