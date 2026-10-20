package updater

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    json "encoding/json/v2"
    "strings"
    "testing"
    "time"
)

func TestGROK004SignedManifestVerify(t *testing.T) {
    pub, priv, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
    base := updateManifest{
        Schema: 1,
        Product: "capimux",
        Channel: "stable",
        Version: "1.9.7",
        IssuedAt: now.Add(-time.Hour),
        ExpiresAt: now.Add(24*time.Hour),
        Assets: map[string]manifestAsset{
            "linux_amd64": {
                URL: "https://example.com/release/9router-go-linux-amd64",
                SHA256: strings.Repeat("ab", 32),
                Size: 2048,
            },
        },
    }
    policy := manifestPolicy{
        TrustedKeys: map[string]ed25519.PublicKey{"release-2026": pub},
        Product: "capimux", Channel: "stable",
        CurrentVersion: "1.9.6", Platform: "linux_amd64", Now: now,
    }
    seal := func(m updateManifest, privateKey ed25519.PrivateKey) []byte {
        t.Helper()
        raw, err := json.Marshal(m)
        if err != nil { t.Fatal(err) }
        msg := append([]byte(manifestSigningContext), raw...)
        sig := ed25519.Sign(privateKey, msg)
        envelope, err := json.Marshal(signedManifestEnvelope{
            Payload: base64.StdEncoding.EncodeToString(raw),
            Signatures: []manifestSignature{{
                KeyID: "release-2026", Sig: base64.StdEncoding.EncodeToString(sig),
            }},
        })
        if err != nil { t.Fatal(err) }
        return envelope
    }
    if got, err := verifySignedManifest(seal(base, priv), policy); err != nil || got.Version != "1.9.7" {
        t.Fatalf("valid signed manifest denied: result=%+v err=%v", got, err)
    }
    _, attackerPriv, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { t.Fatal(err) }
    if _, err := verifySignedManifest(seal(base, attackerPriv), policy); err == nil {
        t.Fatal("untrusted signing key accepted")
    }
    old := base; old.Version = "1.9.6"
    if _, err := verifySignedManifest(seal(old, priv), policy); err == nil {
        t.Fatal("rollback accepted")
    }
    expired := base; expired.ExpiresAt = now.Add(-time.Minute)
    if _, err := verifySignedManifest(seal(expired, priv), policy); err == nil {
        t.Fatal("expired signature accepted")
    }
    wrong := base; wrong.Product = "another-project"
    if _, err := verifySignedManifest(seal(wrong, priv), policy); err == nil {
        t.Fatal("wrong product accepted")
    }
    noKeys := policy; noKeys.TrustedKeys = nil
    if _, err := verifySignedManifest(seal(base, priv), noKeys); err == nil {
        t.Fatal("manifest accepted without a trusted public key")
    }
}
