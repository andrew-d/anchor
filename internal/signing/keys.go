package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
)

const (
	pemTypePrivateKey = "ANCHOR SIGNING KEY"
	pemTypePublicKey  = "ANCHOR PUBLIC KEY"
)

// GenerateKey creates a new ed25519 keypair. Returns the 32-byte seed
// (private key) and the 32-byte public key.
func GenerateKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating ed25519 key: %w", err)
	}
	return priv, pub, nil
}

// MarshalPrivateKey encodes an ed25519 private key seed into Anchor PEM format.
func MarshalPrivateKey(key ed25519.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePrivateKey,
		Bytes: key.Seed(),
	})
}

// MarshalPublicKey encodes an ed25519 public key into Anchor PEM format.
func MarshalPublicKey(key ed25519.PublicKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  pemTypePublicKey,
		Bytes: []byte(key),
	})
}

// ParsePrivateKey decodes a private key from PEM data. Accepts both
// Anchor PEM format (ANCHOR SIGNING KEY) and OpenSSH private key format.
// Returns an error for non-ed25519 keys.
func ParsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	switch block.Type {
	case pemTypePrivateKey:
		if len(block.Bytes) != ed25519.SeedSize {
			return nil, fmt.Errorf("invalid anchor private key: expected %d bytes, got %d", ed25519.SeedSize, len(block.Bytes))
		}
		return ed25519.NewKeyFromSeed(block.Bytes), nil
	case "OPENSSH PRIVATE KEY":
		return parseOpenSSHPrivateKey(data)
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", block.Type)
	}
}

// ParsePublicKey decodes a single public key. Accepts Anchor PEM format
// (ANCHOR PUBLIC KEY) or a single SSH authorized_keys line (ssh-ed25519 AAAA...).
// Returns an error for non-ed25519 keys.
func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block != nil && block.Type == pemTypePublicKey {
		if len(block.Bytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid anchor public key: expected %d bytes, got %d", ed25519.PublicKeySize, len(block.Bytes))
		}
		return ed25519.PublicKey(block.Bytes), nil
	}

	// Try SSH authorized_keys format
	keys := ParsePublicKeys(data)
	if len(keys) == 0 {
		return nil, errors.New("no ed25519 public key found")
	}
	return keys[0], nil
}

// ParsePublicKeys parses multiple public keys from SSH authorized_keys format.
// Each line is expected to be in "ssh-ed25519 AAAA..." format.
// Non-ed25519 keys, blank lines, and comments are silently skipped.
func ParsePublicKeys(data []byte) []ed25519.PublicKey {
	return parseSSHPublicKeys(data)
}
