package entitlements

// StagingSigningKeyID identifies the current test-only Cloudflare signing key.
// This key is public and safe to embed. Production code must opt in explicitly
// to this staging keyring; it is not enabled by default.
const StagingSigningKeyID = "beta-test-20261006-a"

const stagingSigningPublicKeyBase64 = "9i9Elbp+Q/FJ48h50fGryuEDsgqhUAqLFoDuhgt59QE="

// StagingKeyRing returns the public keyring used by the current Beta Pro
// Cloudflare staging control plane.
func StagingKeyRing() (KeyRing, error) {
	return ParseKeyRing(map[string]string{
		StagingSigningKeyID: stagingSigningPublicKeyBase64,
	})
}
