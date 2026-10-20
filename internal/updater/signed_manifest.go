package updater

// GROK-004: verified manifest building block. Deliberately NOT wired to
// CheckUpdate until an offline release key and trusted public key are installed.
import (
    "crypto/ed25519"
    "encoding/base64"
    "encoding/hex"
    json "encoding/json/v2"
    "errors"
    "fmt"
    "net/url"
    "regexp"
    "strings"
    "time"
)

const (
    manifestSigningContext = "capimux-update-manifest-v1\n"
    manifestSchemaVersion = 1
    maxManifestValidity = 30 * 24 * time.Hour
    manifestClockSkew = 5 * time.Minute
)

var (
    errManifestSignature = errors.New("updater: manifest signature invalid")
    errManifestRollback = errors.New("updater: refused old manifest version")
    errManifestExpired = errors.New("updater: manifest expired or not yet valid")
    strictSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type signedManifestEnvelope struct {
    Payload string `json:"payload"`
    Signatures []manifestSignature `json:"signatures"`
}
type manifestSignature struct {
    KeyID string `json:"keyId"`
    Sig string `json:"sig"`
}
type updateManifest struct {
    Schema int `json:"schema"`
    Product string `json:"product"`
    Channel string `json:"channel"`
    Version string `json:"version"`
    IssuedAt time.Time `json:"issuedAt"`
    ExpiresAt time.Time `json:"expiresAt"`
    Notes string `json:"notes,omitempty"`
    Assets map[string]manifestAsset `json:"assets"`
}
type manifestAsset struct {
    URL string `json:"url"`
    SHA256 string `json:"sha256"`
    Size int64 `json:"size"`
}
type manifestPolicy struct {
    TrustedKeys map[string]ed25519.PublicKey
    Product string
    Channel string
    CurrentVersion string
    Platform string
    Now time.Time
}
type verifiedUpdate struct {
    Version string
    Asset manifestAsset
    KeyID string
}

// The caller supplies trusted public keys; no untrusted key material is loaded
// from the same channel as the release being verified.
func verifySignedManifest(envelope []byte, p manifestPolicy) (*verifiedUpdate, error) {
    if len(p.TrustedKeys) == 0 {
        return nil, errors.New("updater: no trusted signing keys configured")
    }
    if len(envelope) > 512<<10 {
        return nil, errors.New("updater: manifest envelope too large")
    }
    var outer signedManifestEnvelope
    if err := json.Unmarshal(envelope, &outer, json.RejectUnknownMembers(true)); err != nil {
        return nil, fmt.Errorf("updater: malformed manifest envelope: %w", err)
    }
    raw, err := base64.StdEncoding.DecodeString(outer.Payload)
    if err != nil || len(raw) == 0 || len(raw) > 256<<10 {
        return nil, errors.New("updater: invalid manifest payload encoding")
    }
    message := append([]byte(manifestSigningContext), raw...)
    signer := ""
    for _, signature := range outer.Signatures {
        pub := p.TrustedKeys[signature.KeyID]
        if len(pub) != ed25519.PublicKeySize {
            continue
        }
        sig, err := base64.StdEncoding.DecodeString(signature.Sig)
        if err != nil || len(sig) != ed25519.SignatureSize {
            continue
        }
        if ed25519.Verify(pub, message, sig) {
            signer = signature.KeyID
            break
        }
    }
    if signer == "" {
        return nil, errManifestSignature
    }
    var m updateManifest
    if err := json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
        return nil, fmt.Errorf("updater: malformed signed payload: %w", err)
    }
    if m.Schema != manifestSchemaVersion || m.Product != p.Product || m.Channel != p.Channel {
        return nil, errors.New("updater: unsupported manifest schema/product/channel")
    }
    if m.IssuedAt.IsZero() || m.ExpiresAt.IsZero() || !m.ExpiresAt.After(m.IssuedAt) ||
        m.ExpiresAt.Sub(m.IssuedAt) > maxManifestValidity ||
        p.Now.Add(manifestClockSkew).Before(m.IssuedAt) || !p.Now.Before(m.ExpiresAt) {
        return nil, errManifestExpired
    }
    if !strictSemver.MatchString(m.Version) ||
        CompareVersions(m.Version, strings.TrimPrefix(p.CurrentVersion, "v")) <= 0 {
        return nil, errManifestRollback
    }
    asset, ok := m.Assets[p.Platform]
    if !ok {
        return nil, fmt.Errorf("updater: missing asset for %s", p.Platform)
    }
    digest, err := hex.DecodeString(asset.SHA256)
    if err != nil || len(digest) != 32 {
        return nil, errors.New("updater: invalid asset SHA-256")
    }
    location, err := url.Parse(asset.URL)
    if err != nil || location.Scheme != "https" || location.Host == "" || location.User != nil {
        return nil, errors.New("updater: update asset must use HTTPS")
    }
    if asset.Size <= 0 || asset.Size > 150<<20 {
        return nil, errors.New("updater: invalid asset size")
    }
    return &verifiedUpdate{Version:m.Version, Asset:asset, KeyID:signer}, nil
}
