package updater

import (
    "bytes"
    "context"
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    json "encoding/json/v2"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func grok004SealedManifest(t *testing.T, now time.Time, private ed25519.PrivateKey) []byte {
    t.Helper()
    manifest := updateManifest{
        Schema: 1, Product: "capimux", Channel: "stable",
        Version: "1.9.7",
        IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(12*time.Hour),
        Assets: map[string]manifestAsset{
            "linux_amd64": {URL:"https://example.com/capimux-linux-amd64",
                SHA256:strings.Repeat("1a", 32), Size:4096},
        },
    }
    raw, err := json.Marshal(manifest)
    if err != nil { t.Fatal(err) }
    message := append([]byte(manifestSigningContext), raw...)
    signature := ed25519.Sign(private, message)
    envelope, err := json.Marshal(signedManifestEnvelope{
        Payload:base64.StdEncoding.EncodeToString(raw),
        Signatures:[]manifestSignature{{
            KeyID:"test-release-key", Sig:base64.StdEncoding.EncodeToString(signature),
        }},
    })
    if err != nil { t.Fatal(err) }
    return envelope
}

func TestGROK004SignedManifestCandidateRequiresPinnedPublisher(t *testing.T) {
    trusted, signer, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    _, untrustedSigner, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    now := time.Now().UTC()
    policy := manifestPolicy{
        TrustedKeys:map[string]ed25519.PublicKey{"test-release-key": trusted},
        Product:"capimux", Channel:"stable",
        CurrentVersion:"1.9.6", Platform:"linux_amd64", Now:now,
    }
    var payload []byte = grok004SealedManifest(t, now, signer)
    tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write(payload)
    }))
    defer tlsServer.Close()
    info, err := checkSignedManifestCandidate(context.Background(), tlsServer.URL, policy, tlsServer.Client())
    if err != nil || info == nil {
        t.Fatalf("valid signed candidate rejected: %+v / %v", info, err)
    }
    if info.DownloadURL != "https://example.com/capimux-linux-amd64" || info.SHA256 != strings.Repeat("1a", 32) || !info.HasUpdate {
        t.Fatalf("manifest did not bind update asset and checksum: %+v", info)
    }
    payload = grok004SealedManifest(t, now, untrustedSigner)
    if _, err := checkSignedManifestCandidate(context.Background(), tlsServer.URL, policy, tlsServer.Client()); err == nil {
        t.Fatal("manifest signed by untrusted key accepted")
    }
    policy.TrustedKeys = nil
    if _, err := checkSignedManifestCandidate(context.Background(), tlsServer.URL, policy, tlsServer.Client()); err == nil {
        t.Fatal("manifest with no trusted publisher was accepted")
    }
}

func TestGROK004SignedManifestCandidateRejectsInsecureTransport(t *testing.T) {
    insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer insecure.Close()
    if _, err := checkSignedManifestCandidate(context.Background(), insecure.URL, manifestPolicy{}, nil); err == nil {
        t.Fatal("HTTP manifest URL accepted")
    }
    for _, src := range []string{"https://example.com@attacker.test/path", "file:///tmp/manifest", "https://example.com/path#fragment"} {
        if src == "https://example.com@attacker.test/path" {
            // A userinfo-style URL must never be allowed.
        }
        if err := validateSignedManifestSource(src); err == nil {
            t.Fatalf("unsafe manifest source accepted: %s", src)
        }
    }
}

func TestGROK004SignedManifestCandidateRejectsHostSwitchAndOversize(t *testing.T) {
    redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        http.Redirect(w, req, "https://example.com/evil", http.StatusFound)
    }))
    defer redirect.Close()
    if _, err := checkSignedManifestCandidate(context.Background(), redirect.URL, manifestPolicy{}, redirect.Client()); err == nil {
        t.Fatal("cross-host signed manifest redirect accepted")
    }
    oversized := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        _, _ = w.Write(bytes.Repeat([]byte("X"), maxSignedManifestDownload+20))
    }))
    defer oversized.Close()
    if _, err := checkSignedManifestCandidate(context.Background(), oversized.URL, manifestPolicy{}, oversized.Client()); err == nil {
        t.Fatal("oversized manifest accepted")
    }
}
