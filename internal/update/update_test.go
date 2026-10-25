package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.10.0", "v1.9.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0", "v1.0.0-6-g32cd1cf-dirty", false},
		{"v1.0.1", "v1.0.0-6-g32cd1cf", true},
		{"v1.0.1", "dev", false},
		{"v1.0.1", "32cd1cf", false},
		{"garbage", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func fakeRelease(t *testing.T, version string, bin []byte, tamper bool) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string][]byte{"README.md": []byte("readme"), "queryme": bin} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	archive := fmt.Sprintf("queryme_%s_%s_%s.tar.gz", version[1:], runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	data := buf.Bytes()
	if tamper {
		data = append(append([]byte{}, data...), 'x')
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/"+version, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), archive)
	})
	mux.HandleFunc("/releases/download/"+version+"/"+archive, func(w http.ResponseWriter, _ *http.Request) { w.Write(data) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := repoURL
	repoURL = srv.URL
	t.Cleanup(func() { repoURL = old })
}

func TestLatestReadsTheRedirect(t *testing.T) {
	fakeRelease(t, "v1.2.3", nil, false)
	if got, err := Latest(context.Background()); err != nil || got != "v1.2.3" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestInstallReplacesTheBinary(t *testing.T) {
	fakeRelease(t, "v1.2.3", []byte("new binary"), false)
	exe := filepath.Join(t.TempDir(), "queryme")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := Install(context.Background(), "v1.2.3", exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new binary" {
		t.Fatalf("binary is %q", got)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
}

func TestInstallRejectsAChecksumMismatch(t *testing.T) {
	fakeRelease(t, "v1.2.3", []byte("new binary"), true)
	exe := filepath.Join(t.TempDir(), "queryme")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := Install(context.Background(), "v1.2.3", exe); err == nil {
		t.Fatal("expected a checksum error")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary was replaced: %q", got)
	}
}
