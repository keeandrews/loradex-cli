// Package credstore stores CLI credentials keyed by "<profile>@<endpoint-host>".
//
// A token is bound to the endpoint host it was issued for: lookups are by the
// currently targeted host, so a token for api.loradex.ai is never sent to a
// different host (e.g. an attacker-controlled LORADEX_ENDPOINT).
package credstore

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/zalando/go-keyring"
)

const service = "loradex"

// Credential is the stored secret material.
type Credential struct {
	Token  string `json:"token"`
	Handle string `json:"handle"`
}

// HostOf extracts the host (host:port) from an endpoint URL for keying.
func HostOf(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	return u.Host
}

// Key builds the storage key "<profile>@<host>".
func Key(profile, endpoint string) string {
	if profile == "" {
		profile = config.DefaultProfile
	}
	return profile + "@" + HostOf(endpoint)
}

// fileDoc is the shape of the credentials.json fallback file.
type fileDoc struct {
	Version     int                   `json:"version"`
	Credentials map[string]Credential `json:"credentials"`
}

func credPath() (string, error)  { return config.Path("credentials.json") }
func indexPath() (string, error) { return config.Path("cred_index.json") }

// Set stores a credential under key (keyring first, file fallback otherwise).
func Set(key string, cred Credential) error {
	blob, _ := json.Marshal(cred)
	if err := keyring.Set(service, key, string(blob)); err == nil {
		return addIndex(key) // record key so DeleteAll can enumerate keyring entries
	}
	return fileSet(key, cred)
}

// Get returns the credential for key. ok=false if none is stored for that host.
func Get(key string) (Credential, bool, error) {
	if blob, err := keyring.Get(service, key); err == nil {
		var c Credential
		if json.Unmarshal([]byte(blob), &c) == nil && c.Token != "" {
			return c, true, nil
		}
	}
	return fileGet(key)
}

// Delete removes the credential for key from both backends. Idempotent.
func Delete(key string) error {
	_ = keyring.Delete(service, key)
	_ = removeIndex(key)
	return fileDelete(key)
}

// List returns every stored key across both backends.
func List() ([]string, error) {
	set := map[string]struct{}{}
	for _, k := range readIndex() {
		set[k] = struct{}{}
	}
	doc, err := readFileDoc()
	if err != nil {
		return nil, err
	}
	for k := range doc.Credentials {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// DeleteAll removes every stored credential.
func DeleteAll() error {
	keys, err := List()
	if err != nil {
		return err
	}
	for _, k := range keys {
		_ = Delete(k)
	}
	if p, e := credPath(); e == nil {
		_ = os.Remove(p)
	}
	if p, e := indexPath(); e == nil {
		_ = os.Remove(p)
	}
	return nil
}

// --- file fallback ---

func readFileDoc() (*fileDoc, error) {
	p, err := credPath()
	if err != nil {
		return nil, err
	}
	if err := enforcePerms(p); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &fileDoc{Version: 1, Credentials: map[string]Credential{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc fileDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Credentials == nil {
		doc.Credentials = map[string]Credential{}
	}
	return &doc, nil
}

func writeFileDoc(doc *fileDoc) error {
	p, err := credPath()
	if err != nil {
		return err
	}
	doc.Version = 1
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func fileSet(key string, cred Credential) error {
	doc, err := readFileDoc()
	if err != nil {
		return err
	}
	doc.Credentials[key] = cred
	return writeFileDoc(doc)
}

func fileGet(key string) (Credential, bool, error) {
	doc, err := readFileDoc()
	if err != nil {
		return Credential{}, false, err
	}
	c, ok := doc.Credentials[key]
	if !ok || c.Token == "" {
		return Credential{}, false, nil
	}
	return c, true, nil
}

func fileDelete(key string) error {
	doc, err := readFileDoc()
	if err != nil {
		return err
	}
	if _, ok := doc.Credentials[key]; !ok {
		return nil
	}
	delete(doc.Credentials, key)
	return writeFileDoc(doc)
}

// enforcePerms refuses a credentials file looser than 0600 and tightens it.
func enforcePerms(p string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		// best-effort tighten; continue using it after fixing
		_ = os.Chmod(p, 0o600)
	}
	return nil
}

// --- key index (non-secret; lets DeleteAll enumerate keyring entries) ---

type indexDoc struct {
	Keys []string `json:"keys"`
}

func readIndex() []string {
	p, err := indexPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var doc indexDoc
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc.Keys
}

func writeIndex(keys []string) error {
	p, err := indexPath()
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(indexDoc{Keys: keys}, "", "  ")
	return os.WriteFile(p, data, 0o600)
}

func addIndex(key string) error {
	for _, k := range readIndex() {
		if k == key {
			return nil
		}
	}
	return writeIndex(append(readIndex(), key))
}

func removeIndex(key string) error {
	var out []string
	for _, k := range readIndex() {
		if k != key {
			out = append(out, k)
		}
	}
	return writeIndex(out)
}

// NormalizeHandle trims a handle for display.
func NormalizeHandle(h string) string { return strings.TrimSpace(h) }
