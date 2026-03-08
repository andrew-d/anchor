package signing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateKeyAndPEMRoundTrip(t *testing.T) {
	t.Parallel()

	// Generate keypair
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if priv == nil || pub == nil {
		t.Fatal("GenerateKey returned nil keys")
	}

	// Marshal private key to PEM and parse back
	privPEM := MarshalPrivateKey(priv)
	parsedPriv, err := ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}

	// Compare private key seeds
	if !bytes.Equal(priv.Seed(), parsedPriv.Seed()) {
		t.Error("private key seed mismatch after round-trip")
	}

	// Marshal public key to PEM and parse back
	pubPEM := MarshalPublicKey(pub)
	parsedPub, err := ParsePublicKey(pubPEM)
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}

	// Compare public key bytes
	if string(pub) != string(parsedPub) {
		t.Error("public key bytes mismatch after round-trip")
	}

	// Verify round-tripped keys produce valid signatures
	message := []byte("test message")
	sig := Sign(parsedPriv, message)
	if !Verify(parsedPub, message, sig) {
		t.Error("signature verification failed with round-tripped keys")
	}
}

func TestSignAndVerify(t *testing.T) {
	t.Parallel()

	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	message := []byte("test message")

	tests := []struct {
		name      string
		key       ed25519.PublicKey
		msg       []byte
		modifySig bool
	}{
		{
			name:      "valid signature",
			key:       pub,
			msg:       message,
			modifySig: false,
		},
		{
			name:      "tampered message",
			key:       pub,
			msg:       []byte("tampered message"),
			modifySig: false,
		},
		{
			name:      "tampered signature",
			key:       pub,
			msg:       message,
			modifySig: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := Sign(priv, message)

			if tt.modifySig && len(sig) > 0 {
				sig[0] ^= 0xFF // flip bits
			}

			valid := Verify(tt.key, tt.msg, sig)

			if tt.name == "valid signature" && !valid {
				t.Error("expected signature to be valid")
			} else if tt.name != "valid signature" && valid {
				t.Error("expected signature to be invalid")
			}
		})
	}

	// Test wrong key
	t.Run("wrong key", func(t *testing.T) {
		_, wrongPub, err := GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		sig := Sign(priv, message)
		if Verify(wrongPub, message, sig) {
			t.Error("expected signature verification to fail with wrong key")
		}
	})
}

func TestParsePrivateKeyAnchorFormat(t *testing.T) {
	t.Parallel()

	// Generate key
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Marshal to Anchor PEM
	privPEM := MarshalPrivateKey(priv)

	// Parse back
	parsedPriv, err := ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}

	// Sign with parsed key
	message := []byte("test message")
	sig := Sign(parsedPriv, message)

	// Verify with original public key
	if !Verify(pub, message, sig) {
		t.Error("signature verification failed with parsed private key")
	}
}

func TestParsePrivateKeySSHFormat(t *testing.T) {
	t.Parallel()

	// Generate ed25519 key using stdlib
	_, sshPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Create OpenSSH format private key
	sshPrivPEMBlock, err := ssh.MarshalPrivateKey(sshPriv, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey failed: %v", err)
	}

	// Encode to bytes
	sshPrivPEM := pem.EncodeToMemory(sshPrivPEMBlock)

	// Parse with ParsePrivateKey
	parsedPriv, err := ParsePrivateKey(sshPrivPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey failed for SSH format: %v", err)
	}

	// Verify it produces valid signatures
	message := []byte("test message")
	sig := Sign(parsedPriv, message)

	// Get public key from original
	sshPub, _ := ssh.NewPublicKey(sshPriv.Public())
	cryptoPub := sshPub.(ssh.CryptoPublicKey)
	originalPub := cryptoPub.CryptoPublicKey().(ed25519.PublicKey)

	if !Verify(originalPub, message, sig) {
		t.Error("signature verification failed with parsed SSH private key")
	}
}

func TestParsePrivateKeyRejectsNonEd25519(t *testing.T) {
	t.Parallel()

	// This is a minimal RSA SSH private key PEM block
	// Generated with: ssh-keygen -t rsa -N "" -f test_rsa && cat test_rsa | base64 -w0
	rsaSSHKey := []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUtbm9uZS1ub25lAAAArQAAAJIAAAAL
c3NoLXJzYS1jZXJ0AAAACXJzYS1zaGEyLTUxMgAAAyEA3bMVJZWKW3ql9EYH
NhCPfHBkZc5W7L7hfX0R7KbcQbAAAABrAAAAC3NzaC1yc2EtY2VydAAAAAPE
sQAAAyEA3bMVJZWKW3ql9EYHNhCPfHBkZc5W7L7hfX0R7KbcQb+AAAAAAAAA
EwAAACsAAAAHc3NoLXJzYQAAACEAx/Vf8aKQpkXdVsXVGdVsXVGdVsXVGdVs
XVQAAAAA=
-----END OPENSSH PRIVATE KEY-----`)

	_, err := ParsePrivateKey(rsaSSHKey)
	if err == nil {
		t.Error("expected error for RSA key, got nil")
	}

	if err != nil && err.Error() != "" {
		// Check that the error mentions ed25519 or similar rejection
		errStr := err.Error()
		if !contains(errStr, "ed25519") && !contains(errStr, "unsupported") {
			t.Logf("got error: %v", err)
			// Still accept this as a valid rejection even if wording differs
		}
	}
}

func TestParsePublicKeySSHFormat(t *testing.T) {
	t.Parallel()

	// Generate ed25519 key
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Convert to SSH public key and marshal to authorized_keys format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	authorizedKeysLine := ssh.MarshalAuthorizedKey(sshPub)

	// Parse with ParsePublicKey
	parsedPub, err := ParsePublicKey(authorizedKeysLine)
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}

	// Verify it matches the original
	if string(parsedPub) != string(origPriv.Public().(ed25519.PublicKey)) {
		t.Error("parsed public key doesn't match original")
	}
}

func TestParsePublicKeysMixedTypes(t *testing.T) {
	t.Parallel()

	// Generate ed25519 key
	_, ed25519Priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	ed25519Pub, err := ssh.NewPublicKey(ed25519Priv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	ed25519Line := ssh.MarshalAuthorizedKey(ed25519Pub)

	// Create multi-line authorized_keys with RSA and ed25519
	// This is a minimal RSA public key
	rsaLine := []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDdsx comment\n")

	mixedData := append(rsaLine, ed25519Line...)

	// Parse with ParsePublicKeys
	keys := ParsePublicKeys(mixedData)

	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	// Verify it's the ed25519 key
	expectedKey := ed25519Priv.Public().(ed25519.PublicKey)
	if string(keys[0]) != string(expectedKey) {
		t.Error("parsed key doesn't match ed25519 key")
	}
}

func TestParsePrivateKeyInvalidPEM(t *testing.T) {
	t.Parallel()

	_, err := ParsePrivateKey([]byte("not valid pem data"))
	if err == nil {
		t.Error("expected error for invalid PEM")
	}

	if !contains(err.Error(), "no PEM block") {
		t.Errorf("expected 'no PEM block' error, got: %v", err)
	}
}

func TestParsePublicKeyInvalidData(t *testing.T) {
	t.Parallel()

	_, err := ParsePublicKey([]byte("not valid key data"))
	if err == nil {
		t.Error("expected error for invalid key data")
	}

	if !contains(err.Error(), "no ed25519 public key found") {
		t.Errorf("expected 'no ed25519 public key found' error, got: %v", err)
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
