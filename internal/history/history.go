package history

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chinhtran52/queryme/internal/config"
	"github.com/chinhtran52/queryme/internal/secret"
)

const maxEntries = 10

// Entry is a past successful connection. The password is only ever written encrypted.
type Entry struct {
	URL               string `json:"url"`
	Name              string `json:"name,omitempty"`
	SSH               string `json:"ssh,omitempty"`
	ReadOnly          bool   `json:"read_only"`
	NeedsPassword     bool   `json:"needs_password"`
	EncryptedPassword string `json:"encrypted_password,omitempty"`
	// EncryptedSSLPassword is the client key passphrase from ?sslpassword=, kept out of URL like the password.
	EncryptedSSLPassword string    `json:"encrypted_ssl_password,omitempty"`
	LastUsed             time.Time `json:"last_used"`
	password             string
	sslPassword          string
}

// MustAskPassword is true when the connection needs a password that could not be restored.
func (e Entry) MustAskPassword() bool { return e.NeedsPassword && e.password == "" }

// Connection puts a restored sslpassword back into the URL, where pgx reads it.
func (e Entry) Connection() config.Connection {
	url := e.URL
	if e.sslPassword != "" {
		url = withSSLPassword(url, e.sslPassword)
	}
	return config.Connection{Name: e.Name, URL: url, SSH: e.SSH, ReadOnly: e.ReadOnly, Password: e.password}
}

type Store struct {
	path    string
	cipher  *secret.Cipher
	entries []Entry
}

func DefaultDir() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "queryme"), nil
}

// Open returns an empty store when the file does not exist yet. A nil cipher means passwords are not saved.
func Open(path string, cipher *secret.Cipher) (*Store, error) {
	s := &Store{path: path, cipher: cipher}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		s.entries = nil
		return s, err
	}
	for i := range s.entries {
		e := &s.entries[i]
		e.password = s.decrypt(e.EncryptedPassword, e.URL)
		e.sslPassword = s.decrypt(e.EncryptedSSLPassword, sslAAD(e.URL))
	}
	return s, nil
}

// decrypt returns "" when the password cannot be restored (new key, tampered file), so the user is asked again.
func (s *Store) decrypt(ciphertext, aad string) string {
	if s.cipher == nil || ciphertext == "" {
		return ""
	}
	plain, err := s.cipher.Decrypt(ciphertext, aad)
	if err != nil {
		return ""
	}
	return plain
}

func (s *Store) SavesPasswords() bool { return s.cipher != nil }

// Entries are ordered most recent first.
func (s *Store) Entries() []Entry { return s.entries }

func (s *Store) Record(c config.Connection, at time.Time) error {
	clean, urlPassword, sslPassword, ok := sanitize(c.URL)
	if !ok {
		return nil
	}
	password := c.Password
	if password == "" {
		password = urlPassword
	}
	entry := Entry{URL: clean, Name: c.Name, SSH: c.SSH, ReadOnly: c.ReadOnly, NeedsPassword: password != "", LastUsed: at, password: password, sslPassword: sslPassword}
	if s.cipher != nil {
		var err error
		if entry.EncryptedPassword, err = s.encrypt(password, clean); err != nil {
			return err
		}
		if entry.EncryptedSSLPassword, err = s.encrypt(sslPassword, sslAAD(clean)); err != nil {
			return err
		}
	}

	entries := []Entry{entry}
	for _, e := range s.entries {
		same := e.URL == clean && e.SSH == c.SSH
		if same && entries[0].Name == "" {
			// Connecting again without giving a name keeps the one set before.
			entries[0].Name = e.Name
		}
		switch {
		case same && s.cipher == nil:
			// A session without a keyring must not wipe a password saved by one that had it.
			entries[0].EncryptedPassword = e.EncryptedPassword
			entries[0].EncryptedSSLPassword = e.EncryptedSSLPassword
		case !same && len(entries) < maxEntries:
			entries = append(entries, e)
		}
	}
	s.entries = entries
	return s.save()
}

// Delete forgets a recent connection and its saved password. removed is false when no entry matches.
func (s *Store) Delete(url, ssh string) (removed bool, err error) {
	kept := s.entries[:0:0]
	for _, e := range s.entries {
		if e.URL == url && e.SSH == ssh {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return false, nil
	}
	s.entries = kept
	return true, s.save()
}

// Rename sets the display name of a recent connection; an empty name shows the URL again.
func (s *Store) Rename(url, ssh, name string) (found bool, err error) {
	for i, e := range s.entries {
		if e.URL == url && e.SSH == ssh {
			s.entries[i].Name = strings.TrimSpace(name)
			return true, s.save()
		}
	}
	return false, nil
}

// Label is the name when one is set, else the URL.
func (e Entry) Label() string {
	if e.Name != "" {
		return e.Name
	}
	return e.URL
}

func (s *Store) encrypt(plain, aad string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return s.cipher.Encrypt(plain, aad)
}

// sslAAD binds the passphrase to its URL and keeps it from being swapped with the password.
func sslAAD(url string) string { return url + "#sslpassword" }

func withSSLPassword(raw, sslPassword string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("sslpassword", sslPassword)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// PasswordFor returns the saved password for a connection URL, or "" when none is saved or readable.
// A URL that carries its own password gets "", so the explicit one wins.
func (s *Store) PasswordFor(rawURL string) string {
	clean, urlPassword, _, ok := sanitize(rawURL)
	if !ok || urlPassword != "" {
		return ""
	}
	for _, e := range s.entries {
		if e.URL == clean {
			return e.password
		}
	}
	return ""
}

var keywordPassword = regexp.MustCompile(`password\s*=\s*('(?:[^'\\]|\\.)*'|\S+)`)

// Redact hides any password or sslpassword in a connection URL or keyword DSN, for display.
func Redact(raw string) string {
	if clean, _, _, ok := sanitize(raw); ok {
		return clean
	}
	return keywordPassword.ReplaceAllString(raw, "password=…")
}

// sanitize strips the password and the SSL key passphrase from a postgres URL and returns them separately.
// shortcut: keyword DSNs ("host=… user=…") and env-only connections are not remembered; parse them too if users ask.
func sanitize(raw string) (clean, password, sslPassword string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", "", "", false
	}
	if p, set := u.User.Password(); set {
		password = p
		u.User = url.User(u.User.Username())
	}
	if q := u.Query(); q.Has("password") || q.Has("sslpassword") {
		if password == "" {
			password = q.Get("password")
		}
		sslPassword = q.Get("sslpassword")
		q.Del("password")
		q.Del("sslpassword")
		u.RawQuery = q.Encode()
	}
	return u.String(), password, sslPassword, true
}
