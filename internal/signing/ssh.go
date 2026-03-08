package signing

import (
	"crypto/ed25519"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// parseOpenSSHPrivateKey extracts an ed25519 private key from OpenSSH
// private key PEM data. Returns an error for non-ed25519 keys or
// passphrase-protected keys.
func parseOpenSSHPrivateKey(pemData []byte) (ed25519.PrivateKey, error) {
	raw, err := ssh.ParseRawPrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH private key: %w", err)
	}

	switch k := raw.(type) {
	case *ed25519.PrivateKey:
		return *k, nil
	case ed25519.PrivateKey:
		return k, nil
	default:
		return nil, fmt.Errorf("unsupported SSH key type %T: only ed25519 keys are supported", raw)
	}
}

// parseSSHPublicKeys parses SSH authorized_keys format data and returns
// all ed25519 public keys found. Non-ed25519 keys, blank lines, and
// comments are silently skipped.
func parseSSHPublicKeys(data []byte) []ed25519.PublicKey {
	var keys []ed25519.PublicKey
	rest := data
	for len(rest) > 0 {
		pub, _, _, remaining, err := ssh.ParseAuthorizedKey(rest)
		rest = remaining
		if err != nil {
			break
		}
		if pub.Type() != ssh.KeyAlgoED25519 {
			continue
		}
		cryptoPub, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			continue
		}
		ed25519Pub, ok := cryptoPub.CryptoPublicKey().(ed25519.PublicKey)
		if !ok {
			continue
		}
		keys = append(keys, ed25519Pub)
	}
	return keys
}
