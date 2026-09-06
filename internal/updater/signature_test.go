package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestFabricSignedUpdate(t *testing.T) {
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("synthetic executable")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
	key := base64.StdEncoding.EncodeToString(pub)
	if e = verifyUpdateSignature(data, sig, key); e != nil {
		t.Fatal(e)
	}
	for _, s := range []struct {
		data     []byte
		sig, key string
	}{{[]byte("tampered"), sig, key}, {data, "", key}, {data, sig, ""}} {
		if verifyUpdateSignature(s.data, s.sig, s.key) == nil {
			t.Fatal("unsigned or tampered accepted")
		}
	}
}
