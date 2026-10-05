package entitlements

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRuntimeStoreConcurrentFirstRunReturnsOneInstallationID(t *testing.T) {
	dataDir := t.TempDir()
	const workers = 64

	start := make(chan struct{})
	results := make(chan string, workers)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, err := NewRuntimeStore(dataDir).InstallationID()
			if err != nil {
				errs <- err
				return
			}
			results <- id
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("InstallationID() error = %v", err)
	}

	var canonical string
	for id := range results {
		if canonical == "" {
			canonical = id
			continue
		}
		if id != canonical {
			t.Fatalf("concurrent callers returned different ids: %q != %q", id, canonical)
		}
	}
	persisted, err := NewRuntimeStore(dataDir).InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if persisted != canonical {
		t.Fatalf("persisted id = %q, returned id = %q", persisted, canonical)
	}
}

func TestRuntimeStoreInstallationIDWaitsForCrossStoreLock(t *testing.T) {
	dataDir := t.TempDir()
	store := NewRuntimeStore(dataDir)
	path := store.installationIDPath()

	entered := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- withPathLock(path, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	result := make(chan string, 1)
	errResult := make(chan error, 1)
	go func() {
		id, err := NewRuntimeStore(dataDir).InstallationID()
		if err != nil {
			errResult <- err
			return
		}
		result <- id
	}()

	select {
	case id := <-result:
		t.Fatalf("InstallationID returned %q while lock was held", id)
	case err := <-errResult:
		t.Fatalf("InstallationID failed while waiting for lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errResult:
		t.Fatal(err)
	case id := <-result:
		if id == "" {
			t.Fatal("empty installation id")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InstallationID did not resume after lock release")
	}
}

func TestRuntimeStoreConcurrentTrustedTimeKeepsMaximum(t *testing.T) {
	dataDir := t.TempDir()
	base := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if _, err := NewRuntimeStore(dataDir).AdvanceTrustedTime(base); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		candidate := base.Add(time.Duration(i) * time.Minute)
		wg.Add(1)
		go func(at time.Time) {
			defer wg.Done()
			<-start
			_, err := NewRuntimeStore(dataDir).AdvanceTrustedTime(at)
			if err != nil {
				errs <- err
			}
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	got, err := NewRuntimeStore(dataDir).LastTrustedTime()
	if err != nil {
		t.Fatal(err)
	}
	want := base.Add(workers * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("trusted time = %s, want maximum %s", got, want)
	}
}

func TestRuntimeStoreRecoversAbandonedLock(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	path := store.installationIDPath()
	if err := os.MkdirAll(store.Dir(), privateDirPerm); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("stale\n"), privateFilePerm); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * runtimeLockStale)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	id, err := store.InstallationID()
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty installation id after stale lock recovery")
	}
}


func TestRuntimeStoreDoesNotAbandonLiveOwnerAfterClockJump(t *testing.T) {
	store := NewRuntimeStore(t.TempDir())
	path := store.installationIDPath()
	if err := os.MkdirAll(store.Dir(), privateDirPerm); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), privateFilePerm); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * runtimeLockStale)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if abandonedRuntimeLock(lockPath) {
		t.Fatal("live lock owner was considered abandoned solely because of wall-clock age")
	}
}
