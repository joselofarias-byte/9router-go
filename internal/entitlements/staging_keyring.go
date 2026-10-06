package entitlements

// StagingSigningKeyID identifies the current test-only Cloudflare signing key.
// This key is public and safe to embed. Production code must opt in explicitly
// to this staging keyring; it is not enabled by default.
const StagingSigningKeyID = "beta-test-20261006-b"

const stagingSigningPublicKeyBase64 = "hSUsFZEQ4Mp9ua9oNis1GdhMVvzTFfQZC9Ez7jbhhgw="

// StagingKeyRing returns the public keyring used by the current Beta Pro
// Cloudflare staging control plane.
func StagingKeyRing() (KeyRing, error) {
	return ParseKeyRing(map[string]string{
		StagingSigningKeyID: stagingSigningPublicKeyBase64,
	})
}
