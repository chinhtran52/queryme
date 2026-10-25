// Package update finds the latest queryme release on GitHub and replaces the running binary with it.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// repoURL is a var so tests can point it at a fake server.
var repoURL = "https://github.com/chinhtran52/queryme"

// Latest returns the newest release tag, read from the /releases/latest redirect to skip the rate-limited API.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, repoURL+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	tag := resp.Header.Get("Location")
	tag = tag[strings.LastIndex(tag, "/")+1:]
	if _, ok := parse(tag); !ok {
		return "", fmt.Errorf("no release found (%s)", resp.Status)
	}
	return tag, nil
}

// Newer reports whether latest is a higher release than current. A current version that isn't vX.Y.Z (dev builds) never is.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// Released reports whether v looks like a release version, so it can be compared with Newer.
func Released(v string) bool {
	_, ok := parse(v)
	return ok
}

// parse reads vX.Y.Z, ignoring a -suffix such as git describe's "-6-gabc123".
func parse(v string) ([3]int, bool) {
	var n [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(core, ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// Install downloads release version for this OS and CPU, checks it against the release checksums,
// and atomically replaces the binary at exe.
func Install(ctx context.Context, version, exe string) error {
	archive := fmt.Sprintf("queryme_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	base := repoURL + "/releases/download/" + version + "/"
	sums, err := fetch(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksum(sums, archive)
	if err != nil {
		return err
	}
	data, err := fetch(ctx, base+archive)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", archive)
	}
	bin, err := extract(data)
	if err != nil {
		return err
	}
	// Write next to exe so the rename stays on one filesystem and never leaves a half-written binary.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".queryme-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func checksum(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("%s is not listed in checksums.txt", name)
}

func extract(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("queryme binary not found in the release archive")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && h.Name == "queryme" {
			return io.ReadAll(tr)
		}
	}
}
