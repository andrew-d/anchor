package signing

import (
	"crypto/ed25519"
)

// Verify checks that the signature of message is valid for the given public key.
func Verify(key ed25519.PublicKey, message, signature []byte) bool {
	return ed25519.Verify(key, message, signature)
}
