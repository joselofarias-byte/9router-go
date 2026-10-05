package entitlements

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	runtimeDirName       = "license"
	installationIDName   = "installation-id"
	leaseCacheName       = "lease.json"
	lastTrustedTimeName  = "last-trusted-time"
	privateDirPerm       = 0700
	privateFilePerm      = 0600
)

// RuntimeStore keeps only public/signed entitlement runtime state below the
// configured 9router DATA_DIR. It never stores provider credentials, payment
// credentials, activation codes or private signing keys.
type RuntimeStore struct {
	dir string
}

// NewRuntimeStore uses <DATA_DIR>/license as the entitlement runtime directory.
func NewRuntimeStore(dataDir string) *RuntimeStore {
	return &RuntimeStore{dir: filepath.Join(dataDir, runtimeDirName)}
}

func (s *RuntimeStore) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *RuntimeStore) installationIDPath() string {
	return filepath.Join(s.dir, installationIDName)
}

func (s *RuntimeStore) leaseCachePath() string {
	return filepath.Join(s.dir, leaseCacheName)
}

func (s *RuntimeStore) lastTrustedTimePath() string {
	return filepath.Join(s.dir, lastTrustedTimeName)
}

// InstallationID returns one persisted random UUID for this data directory.
// It is intentionally unrelated to IMEI, MAC, Android ID, disk serial or any
// other hardware fingerprint.
func (s *RuntimeStore) InstallationID() (string, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return "", errors.New("entitlements.RuntimeStore: empty data directory")
	}

	raw, err := readRecoverable(s.installationIDPath())
	if err == nil {
		return parseInstallationID(raw)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("entitlements.RuntimeStore.InstallationID: %w", err)
	}

	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("entitlements.RuntimeStore.InstallationID: generate uuid: %w", err)
	}
	value := id.String()
	if err := atomicWriteFile(s.installationIDPath(), []byte(value+"\n")); err != nil {
		return "", fmt.Errorf("entitlements.RuntimeStore.InstallationID: persist uuid: %w", err)
	}

	// Re-read the canonical file so a concurrent first-run writer cannot leave
	// this caller holding an identity different from the one persisted.
	raw, err = readRecoverable(s.installationIDPath())
	if err != nil {
		return "", fmt.Errorf("entitlements.RuntimeStore.InstallationID: reload uuid: %w", err)
	}
	return parseInstallationID(raw)
}

func parseInstallationID(raw []byte) (string, error) {
	value := strings.TrimSpace(string(raw))
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("invalid installation UUID")
	}
	return id.String(), nil
}

// SaveLease atomically replaces the cached signed lease. The cache is not an
// authority: VerifySignedLease remains authoritative every time it is loaded.
func (s *RuntimeStore) SaveLease(raw []byte) error {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return errors.New("entitlements.RuntimeStore: empty data directory")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return errors.New("entitlements.RuntimeStore.SaveLease: empty lease")
	}
	if err := atomicWriteFile(s.leaseCachePath(), append(raw, '\n')); err != nil {
		return fmt.Errorf("entitlements.RuntimeStore.SaveLease: %w", err)
	}
	return nil
}

// LoadLease returns the cached signed lease bytes. os.ErrNotExist means first
// run / Community state and is not treated as a licensing failure by LoadRuntime.
func (s *RuntimeStore) LoadLease() ([]byte, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, errors.New("entitlements.RuntimeStore: empty data directory")
	}
	raw, err := readRecoverable(s.leaseCachePath())
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("entitlements.RuntimeStore.LoadLease: empty lease cache")
	}
	return append([]byte(nil), raw...), nil
}

// LastTrustedTime returns zero time when no trusted clock has been persisted.
func (s *RuntimeStore) LastTrustedTime() (time.Time, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return time.Time{}, errors.New("entitlements.RuntimeStore: empty data directory")
	}
	raw, err := readRecoverable(s.lastTrustedTimePath())
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	value := strings.TrimSpace(string(raw))
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid last trusted time: %w", err)
	}
	return t.UTC(), nil
}

// AdvanceTrustedTime only moves the persisted clock forward.
func (s *RuntimeStore) AdvanceTrustedTime(candidate time.Time) (time.Time, error) {
	if candidate.IsZero() {
		return s.LastTrustedTime()
	}
	candidate = candidate.UTC()

	current, err := s.LastTrustedTime()
	if err != nil {
		return time.Time{}, err
	}
	if !current.IsZero() && !candidate.After(current) {
		return current, nil
	}
	if err := atomicWriteFile(s.lastTrustedTimePath(), []byte(candidate.Format(time.RFC3339Nano)+"\n")); err != nil {
		return time.Time{}, fmt.Errorf("entitlements.RuntimeStore.AdvanceTrustedTime: %w", err)
	}
	return candidate, nil
}

// RuntimeOptions supplies public verification state. Now is injectable only for
// deterministic tests; production callers normally omit it.
type RuntimeOptions struct {
	Keys  KeyRing
	Build BuildIdentity
	Now   func() time.Time
}

// RuntimeState is safe for the application to consume. Provider is always
// non-nil and falls back to Community on any entitlement validation failure.
type RuntimeState struct {
	Provider        Provider
	InstallationID  string
	LastTrustedTime time.Time
}

// LoadRuntime loads the local installation identity and cached lease, verifies
// it offline and returns a provider. Missing lease is a normal Community state.
// Corruption/tampering returns Community plus an error for logging/diagnostics.
func (s *RuntimeStore) LoadRuntime(options RuntimeOptions) (*RuntimeState, error) {
	state := &RuntimeState{Provider: CommunityProvider{}}

	installationID, err := s.InstallationID()
	if err != nil {
		return state, err
	}
	state.InstallationID = installationID

	lastTrusted, err := s.LastTrustedTime()
	if err != nil {
		return state, err
	}
	state.LastTrustedTime = lastTrusted

	raw, err := s.LoadLease()
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}

	nowFn := options.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()

	evaluation, err := VerifySignedLease(raw, options.Keys, VerificationContext{
		Now:             now,
		LastTrustedTime: lastTrusted,
		InstallationID:  installationID,
		Build:           options.Build,
	})
	if err != nil {
		return state, err
	}

	provider := NewLeaseProvider(evaluation)
	provider.now = nowFn
	state.Provider = provider

	advanced, err := s.AdvanceTrustedTime(now)
	if err != nil {
		// The lease was valid, but failing to preserve the rollback guard is a
		// reason to fail closed for Pro rather than continue with weak state.
		state.Provider = CommunityProvider{}
		return state, err
	}
	state.LastTrustedTime = advanced
	return state, nil
}

// atomicWriteFile writes, fsyncs and renames from a private temp file. POSIX
// rename replaces atomically. On platforms where rename cannot replace an
// existing target, the old file is first moved to a recoverable backup and is
// restored automatically if the process stops between replacement steps.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, privateDirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(privateFilePerm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	} else if _, statErr := os.Stat(path); statErr != nil {
		return err
	}

	backup := replacementBackup(path)
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func replacementBackup(path string) string {
	return path + ".replace-old"
}

// readRecoverable restores the previous complete file if a replacement was
// interrupted after moving the old target aside but before publishing the new
// one. Stale temp files are deliberately ignored.
func readRecoverable(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	backup := replacementBackup(path)
	if _, backupErr := os.Stat(backup); backupErr != nil {
		return nil, err
	}
	if renameErr := os.Rename(backup, path); renameErr != nil {
		return nil, renameErr
	}
	return os.ReadFile(path)
}
