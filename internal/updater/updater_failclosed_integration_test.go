package updater

import (
    "bytes"
    "context"
    json "encoding/json/v2"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
)

func grok004AssetServer(t *testing.T, payload []byte) (*httptest.Server, *atomic.Int64) {
    t.Helper()
    var hits atomic.Int64
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        hits.Add(1)
        w.Header().Set("Content-Type", "application/octet-stream")
        _, _ = w.Write(payload)
    }))
    t.Cleanup(server.Close)
    return server, &hits
}

func grok004FakeELF() []byte {
    return append([]byte{0x7f, 'E', 'L', 'F'}, bytes.Repeat([]byte("synthetic-update-payload"), 128)...)
}

func grok004SnapshotExecutable(t *testing.T) (string, int) {
    t.Helper()
    exe, err := os.Executable()
    if err != nil { t.Fatal(err) }
    exe, err = filepath.EvalSymlinks(exe)
    if err != nil { t.Fatal(err) }
    raw, err := os.ReadFile(exe)
    if err != nil { t.Fatal(err) }
    adjacent, err := filepath.Glob(filepath.Join(filepath.Dir(exe), ".9router-go-update-*.tmp"))
    if err != nil { t.Fatal(err) }
    if _, err := os.Stat(exe + ".old"); err == nil {
        adjacent = append(adjacent, exe + ".old")
    }
    return ComputeSHA256(raw), len(adjacent)
}

func TestGROK004PerformSelfUpdateDeniesMissingChecksumBeforeDownload(t *testing.T) {
    server, hits := grok004AssetServer(t, grok004FakeELF())
    previous, files := grok004SnapshotExecutable(t)
    for _, checksum := range []string{"", "short", strings.Repeat("g", 64), " " + strings.Repeat("a", 64)} {
        if err := PerformSelfUpdate(server.URL+"/9router-go-linux-amd64", checksum); err == nil {
            t.Fatalf("accepted invalid checksum with length %d", len(checksum))
        }
    }
    if hits.Load() != 0 { t.Fatal("invalid checksum triggered a download") }
    after, afterFiles := grok004SnapshotExecutable(t)
    if after != previous || afterFiles != files {
        t.Fatal("refused update changed the test executable or left update artifacts")
    }
}

func TestGROK004PerformSelfUpdateMismatchedChecksumCannotReplaceBinary(t *testing.T) {
    server, hits := grok004AssetServer(t, grok004FakeELF())
    previous, files := grok004SnapshotExecutable(t)
    err := PerformSelfUpdate(server.URL+"/9router-go-linux-amd64", strings.Repeat("0", 64))
    if err == nil || !strings.Contains(err.Error(), "mismatch") {
        t.Fatalf("expected hash mismatch, got %v", err)
    }
    if hits.Load() != 1 { t.Fatalf("download count = %d, want 1", hits.Load()) }
    after, afterFiles := grok004SnapshotExecutable(t)
    if after != previous || afterFiles != files {
        t.Fatal("mismatched checksum changed test executable or left update artifacts")
    }
}

func TestGROK004ReleasesFallbackCannotInstallWithoutDigest(t *testing.T) {
    assetServer, hits := grok004AssetServer(t, grok004FakeELF())
    releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.MarshalWrite(w, map[string]any{
            "tag_name": "v99.0.0",
            "assets": []map[string]any{
                {"name": "9router-go-linux-amd64", "browser_download_url": assetServer.URL + "/9router-go-linux-amd64"},
                {"name": "9router-go-darwin-arm64", "browser_download_url": assetServer.URL + "/9router-go-darwin-arm64"},
                {"name": "9router-go-windows-amd64.exe", "browser_download_url": assetServer.URL + "/9router-go-windows-amd64.exe"},
            },
        })
    }))
    defer releases.Close()
    info, err := checkGitHubReleases(context.Background(), releases.URL)
    if err != nil { t.Fatal(err) }
    if !info.HasUpdate || info.DownloadURL == "" {
        t.Fatalf("expected update notice, got %+v", info)
    }
    if info.SHA256 != "" { t.Fatal("fallback now includes a checksum; revisit test") }
    previous, files := grok004SnapshotExecutable(t)
    if err := PerformSelfUpdate(info.DownloadURL, info.SHA256); err == nil {
        t.Fatal("unsigned fallback installed an update")
    }
    if hits.Load() != 0 { t.Fatal("fallback asset was downloaded") }
    after, afterFiles := grok004SnapshotExecutable(t)
    if after != previous || afterFiles != files {
        t.Fatal("rejected release changed test executable")
    }
}
