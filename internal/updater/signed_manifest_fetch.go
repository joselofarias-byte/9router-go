package updater

// Candidate signed-manifest discovery path. This is deliberately not wired to
// CheckUpdate; the release signing root of trust has not been provisioned.
import (
    "context"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "runtime"
    "strings"
    "time"
)

const maxSignedManifestDownload = 512 << 10

func validateSignedManifestSource(raw string) error {
    parsed, err := url.Parse(raw)
    if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
        parsed.User != nil || parsed.Fragment != "" {
        return errors.New("updater: signed manifest URL must be absolute HTTPS")
    }
    return nil
}

// checkSignedManifestCandidate checks a signed manifest against an independently
// pinned trust policy. The caller must supply trusted public keys. Redirects to
// another host or to HTTP fail closed even when HTTP client allows redirects.
// Successful lookup still does not install an executable.
func checkSignedManifestCandidate(ctx context.Context, rawURL string, policy manifestPolicy, suppliedClient *http.Client) (*UpdateInfo, error) {
    if err := validateSignedManifestSource(rawURL); err != nil {
        return nil, err
    }
    manifestURL, _ := url.Parse(rawURL)
    originalHost := strings.ToLower(manifestURL.Host)
    var client http.Client
    if suppliedClient != nil {
        client = *suppliedClient
    } else {
        client.Timeout = 10 * time.Second
    }
    previousRedirect := client.CheckRedirect
    client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
        if err := validateSignedManifestSource(req.URL.String()); err != nil {
            return err
        }
        if !strings.EqualFold(req.URL.Host, originalHost) {
            return errors.New("updater: signed manifest redirect changed host")
        }
        if len(via) >= 5 {
            return errors.New("updater: too many signed manifest redirects")
        }
        if previousRedirect != nil {
            return previousRedirect(req, via)
        }
        return nil
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
    if err != nil {
        return nil, err
    }
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("updater: fetch signed manifest: %w", err)
    }
    defer resp.Body.Close()
    if err := validateSignedManifestSource(resp.Request.URL.String()); err != nil {
        return nil, err
    }
    if !strings.EqualFold(resp.Request.URL.Host, originalHost) {
        return nil, errors.New("updater: unexpected signed manifest host")
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("updater: signed manifest HTTP %d", resp.StatusCode)
    }
    body, err := io.ReadAll(io.LimitReader(resp.Body, maxSignedManifestDownload+1))
    if err != nil {
        return nil, fmt.Errorf("updater: read signed manifest: %w", err)
    }
    if len(body) > maxSignedManifestDownload {
        return nil, errors.New("updater: signed manifest response too large")
    }
    verified, err := verifySignedManifest(body, policy)
    if err != nil {
        return nil, err
    }
    return &UpdateInfo{
        CurrentVersion: policy.CurrentVersion,
        LatestVersion: verified.Version,
        HasUpdate: true,
        DownloadURL: verified.Asset.URL,
        SHA256: verified.Asset.SHA256,
        Source: "signed_manifest_candidate",
        GoVersion: runtime.Version(),
        OS: runtime.GOOS,
        Arch: runtime.GOARCH,
        CheckedAt: time.Now().UTC().Format(time.RFC3339),
    }, nil
}
