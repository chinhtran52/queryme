package history

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/secret"
)

func newCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, err := secret.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSanitizeSeparatesPasswords(t *testing.T) {
	cases := map[string]struct {
		clean, password, sslPassword string
		ok                           bool
	}{
		"postgres://app:s3cret@db:5432/app?sslmode=require":                   {"postgres://app@db:5432/app?sslmode=require", "s3cret", "", true},
		"postgresql://app@db/app?password=s3cret":                             {"postgresql://app@db/app", "s3cret", "", true},
		"postgres://app@db/app?sslmode=verify-full&sslpassword=k3y&sslkey=/k": {"postgres://app@db/app?sslkey=%2Fk&sslmode=verify-full", "", "k3y", true},
		"postgres://app@db/app":                                               {"postgres://app@db/app", "", "", true},
		"host=db user=app password=s3cret":                                    {"", "", "", false},
		"":                                                                    {"", "", "", false},
	}
	for raw, want := range cases {
		clean, password, sslPassword, ok := sanitize(raw)
		if clean != want.clean || password != want.password || sslPassword != want.sslPassword || ok != want.ok {
			t.Errorf("sanitize(%q) = %q, %q, %q, %v", raw, clean, password, sslPassword, ok)
		}
	}
	if got := Redact("host=db sslpassword=k3y password=s3cret"); strings.Contains(got, "k3y") || strings.Contains(got, "s3cret") {
		t.Errorf("Redact leaked: %q", got)
	}
}

func TestRecordDedupesAndCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "connections.json")
	s, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for i := range maxEntries + 2 {
		if err := s.Record(config.Connection{URL: fmt.Sprintf("postgres://u@h/db%d", i)}, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Record(config.Connection{URL: "postgres://u@h/db5", ReadOnly: true}, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Entries()
	if len(got) != maxEntries || got[0].URL != "postgres://u@h/db5" || !got[0].ReadOnly {
		t.Fatalf("entries = %+v", got)
	}
	for _, e := range got[1:] {
		if e.URL == "postgres://u@h/db5" {
			t.Fatal("duplicate entry kept")
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestPasswordsAreSavedEncryptedAndRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	cipher := newCipher(t)
	s, _ := Open(path, cipher)
	if err := s.Record(config.Connection{URL: "postgres://a:from-url@h/one"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(config.Connection{URL: "postgres://b@h/two", Password: "typed-pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "from-url") || strings.Contains(string(data), "typed-pw") {
		t.Fatalf("plaintext password in file:\n%s", data)
	}

	reopened, _ := Open(path, cipher)
	byURL := map[string]config.Connection{}
	for _, e := range reopened.Entries() {
		if e.MustAskPassword() {
			t.Errorf("%s should not ask for a password", e.URL)
		}
		byURL[e.URL] = e.Connection()
	}
	if byURL["postgres://a@h/one"].Password != "from-url" || byURL["postgres://b@h/two"].Password != "typed-pw" {
		t.Fatalf("restored = %+v", byURL)
	}
}

func TestUnreadablePasswordsFallBackToAsking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, _ := Open(path, newCipher(t))
	if err := s.Record(config.Connection{URL: "postgres://a@h/db", Password: "pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	for name, cipher := range map[string]*secret.Cipher{"other key": newCipher(t), "no key": nil} {
		reopened, err := Open(path, cipher)
		if err != nil {
			t.Fatal(err)
		}
		if e := reopened.Entries()[0]; !e.MustAskPassword() || e.Connection().Password != "" {
			t.Errorf("%s: entry = %+v", name, e)
		}
	}
}

func TestWithoutCipherPasswordsAreNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, _ := Open(path, nil)
	if err := s.Record(config.Connection{URL: "postgres://a@h/db", Password: "pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "pw\"") || strings.Contains(string(data), "encrypted_password") {
		t.Fatalf("password saved without a cipher:\n%s", data)
	}
}

func TestSessionWithoutKeyringKeepsSavedPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	cipher := newCipher(t)
	s, _ := Open(path, cipher)
	if err := s.Record(config.Connection{URL: "postgres://a@h/db", Password: "pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	noKeyring, _ := Open(path, nil)
	if !noKeyring.Entries()[0].MustAskPassword() {
		t.Fatal("without a keyring the password must be asked")
	}
	if err := noKeyring.Record(config.Connection{URL: "postgres://a@h/db", Password: "pw"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	withKeyring, _ := Open(path, cipher)
	if got := withKeyring.Entries()[0].Connection().Password; got != "pw" {
		t.Fatalf("saved password lost, got %q", got)
	}
}

func TestRecordSkipsKeywordDSN(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "connections.json"), nil)
	if err := s.Record(config.Connection{URL: "host=db password=x"}, time.Now()); err != nil || len(s.Entries()) != 0 {
		t.Fatalf("err=%v entries=%v", err, s.Entries())
	}
}

func TestDeleteForgetsEntryAndPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	cipher := newCipher(t)
	s, _ := Open(path, cipher)
	for _, url := range []string{"postgres://a@h/keep", "postgres://a@h/drop"} {
		if err := s.Record(config.Connection{URL: url, Password: "pw-" + url[len(url)-4:]}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	if removed, err := s.Delete("postgres://a@h/drop", ""); !removed || err != nil {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if removed, err := s.Delete("postgres://a@h/missing", ""); removed || err != nil {
		t.Fatalf("missing: removed=%v err=%v", removed, err)
	}

	reopened, _ := Open(path, cipher)
	if got := reopened.Entries(); len(got) != 1 || got[0].URL != "postgres://a@h/keep" {
		t.Fatalf("entries = %+v", got)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "drop") {
		t.Fatalf("deleted entry still on disk:\n%s", data)
	}
}

func TestSSLPasswordIsSavedEncryptedAndRestoredIntoURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	cipher := newCipher(t)
	s, _ := Open(path, cipher)
	if err := s.Record(config.Connection{URL: "postgres://a@h/db?sslmode=verify-full&sslpassword=K3YPASS"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "K3YPASS") {
		t.Fatalf("plaintext sslpassword in file:\n%s", data)
	}

	reopened, _ := Open(path, cipher)
	e := reopened.Entries()[0]
	if e.URL != "postgres://a@h/db?sslmode=verify-full" || e.MustAskPassword() {
		t.Fatalf("entry = %+v", e)
	}
	if got := e.Connection().URL; !strings.Contains(got, "sslpassword=K3YPASS") || !strings.Contains(got, "sslmode=verify-full") {
		t.Fatalf("restored URL = %q", got)
	}

	if noKey, _ := Open(path, nil); strings.Contains(noKey.Entries()[0].Connection().URL, "sslpassword") {
		t.Fatal("sslpassword restored without the key")
	}
}

func TestSSHIsPartOfTheEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, _ := Open(path, nil)
	for _, c := range []config.Connection{
		{URL: "postgres://a@db/app"},
		{URL: "postgres://a@db/app", SSH: "me@bastion"},
		{URL: "postgres://a@db/app", SSH: "me@bastion", ReadOnly: true},
	} {
		if err := s.Record(c, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	reopened, _ := Open(path, nil)
	got := reopened.Entries()
	if len(got) != 2 || got[0].SSH != "me@bastion" || !got[0].ReadOnly || got[1].SSH != "" {
		t.Fatalf("entries = %+v", got)
	}
	if c := got[0].Connection(); c.SSH != "me@bastion" {
		t.Fatalf("connection = %+v", c)
	}

	if removed, _ := reopened.Delete("postgres://a@db/app", "me@bastion"); !removed {
		t.Fatal("tunnelled entry not removed")
	}
	if got := reopened.Entries(); len(got) != 1 || got[0].SSH != "" {
		t.Fatalf("direct entry should remain: %+v", got)
	}
}

func TestNameIsOptionalKeptAndRenamable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, _ := Open(path, nil)
	if err := s.Record(config.Connection{URL: "postgres://a@db/app", Name: "prod"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(config.Connection{URL: "postgres://a@db/app"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if e := s.Entries()[0]; e.Name != "prod" || e.Label() != "prod" || e.Connection().Name != "prod" {
		t.Fatalf("reconnecting without a name lost it: %+v", e)
	}

	if found, err := s.Rename("postgres://a@db/app", "", "  replica "); !found || err != nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if found, _ := s.Rename("postgres://a@db/app", "me@bastion", "x"); found {
		t.Fatal("rename matched an entry with another tunnel")
	}
	reopened, _ := Open(path, nil)
	if e := reopened.Entries()[0]; e.Name != "replica" {
		t.Fatalf("after rename: %+v", e)
	}

	_, _ = reopened.Rename("postgres://a@db/app", "", "")
	if e := reopened.Entries()[0]; e.Label() != "postgres://a@db/app" {
		t.Fatalf("empty name should show the URL: %+v", e)
	}
}
