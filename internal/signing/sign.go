package signing

import (
	"crypto/ed25519"
)

// Sign produces an ed25519 signature of the given message using the private key.
func Sign(key ed25519.PrivateKey, message []byte) []byte {
	return ed25519.Sign(key, message)
}
