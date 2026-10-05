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
	runtimeLockRetry     = 10 * time.Millisecond
	runtimeLockTimeout   = 5 * time.Second
	runtimeLockStale     = 30 * time.Second
)

var ErrRuntimeStoreLockTimeout = errors.New("entitlement runtime store lock timeout")

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

	path := s.installationIDPath()
	var value string
	err := withPathLock(path, func() error {
		raw, err := readRecoverable(path)
		if err == nil {
			value, err = parseInstallationID(raw)
			return err
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		id, err := uuid.NewRandom()
		if err != nil {
			return fmt.Errorf("generate uuid: %w", err)
		}
		if err := atomicWriteFile(path, []byte(id.String()+"\n")); err != nil {
			return fmt.Errorf("persist uuid: %w", err)
		}

		// Read the canonical file while still holding the cross-process lock so
		// every concurrent first-run caller returns the same published UUID.
		raw, err = readRecoverable(path)
		if err != nil {
			return fmt.Errorf("reload uuid: %w", err)
		}
		value, err = parseInstallationID(raw)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("entitlements.RuntimeStore.InstallationID: %w", err)
	}
	return value, nil
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
	path := s.leaseCachePath()
	if err := withPathLock(path, func() error {
		return atomicWriteFile(path, append(raw, '\n'))
	}); err != nil {
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
	path := s.leaseCachePath()
	var out []byte
	err := withPathLock(path, func() error {
		raw, err := readRecoverable(path)
		if err != nil {
			return err
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			return errors.New("entitlements.RuntimeStore.LoadLease: empty lease cache")
		}
		out = append([]byte(nil), raw...)
		return nil
	})
	return out, err
}

// LastTrustedTime returns zero time when no trusted clock has been persisted.
func (s *RuntimeStore) LastTrustedTime() (time.Time, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return time.Time{}, errors.New("entitlements.RuntimeStore: empty data directory")
	}
	path := s.lastTrustedTimePath()
	var out time.Time
	err := withPathLock(path, func() error {
		var err error
		out, err = readTrustedTimeUnlocked(path)
		return err
	})
	return out, err
}

func readTrustedTimeUnlocked(path string) (time.Time, error) {
	raw, err := readRecoverable(path)
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

// AdvanceTrustedTime serializes the read/check/write transaction across
// goroutines and processes sharing the same DATA_DIR, so the persisted maximum
// can never be overwritten by an older candidate.
func (s *RuntimeStore) AdvanceTrustedTime(candidate time.Time) (time.Time, error) {
	if candidate.IsZero() {
		return s.LastTrustedTime()
	}
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return time.Time{}, errors.New("entitlements.RuntimeStore: empty data directory")
	}
	candidate = candidate.UTC()
	path := s.lastTrustedTimePath()

	var out time.Time
	err := withPathLock(path, func() error {
		current, err := readTrustedTimeUnlocked(path)
		if err != nil {
			return err
		}
		if !current.IsZero() && !candidate.After(current) {
			out = current
			return nil
		}
		if err := atomicWriteFile(path, []byte(candidate.Format(time.RFC3339Nano)+"\n")); err != nil {
			return err
		}
		out = candidate
		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("entitlements.RuntimeStore.AdvanceTrustedTime: %w", err)
	}
	return out, nil
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


// withPathLock serializes one runtime-state file across goroutines and
// processes using an atomic O_EXCL lock file. Operations are deliberately
// short; an old lock is considered abandoned after runtimeLockStale so a
// crashed process cannot permanently brick Community startup.
func withPathLock(path string, fn func() error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, privateDirPerm); err != nil {
		return err
	}

	lockPath := path + ".lock"
	deadline := time.Now().Add(runtimeLockTimeout)

	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFilePerm)
		if err == nil {
			_, _ = fmt.Fprintf(lock, "%d\n", os.Getpid())
			_ = lock.Sync()
			_ = lock.Close()
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}

		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > runtimeLockStale {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return ErrRuntimeStoreLockTimeout
		}
		time.Sleep(runtimeLockRetry)
	}
}
