package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"strings"
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
		wantValid bool
	}{
		{
			name:      "valid signature",
			key:       pub,
			msg:       message,
			modifySig: false,
			wantValid: true,
		},
		{
			name:      "tampered message",
			key:       pub,
			msg:       []byte("tampered message"),
			modifySig: false,
			wantValid: false,
		},
		{
			name:      "tampered signature",
			key:       pub,
			msg:       message,
			modifySig: true,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sig := Sign(priv, message)

			if tt.modifySig && len(sig) > 0 {
				sig[0] ^= 0xFF // flip bits
			}

			valid := Verify(tt.key, tt.msg, sig)

			if valid != tt.wantValid {
				t.Errorf("Verify() = %v, want %v", valid, tt.wantValid)
			}
		})
	}

	// Test wrong key
	t.Run("wrong key", func(t *testing.T) {
		t.Parallel()

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

	// Generate a real RSA SSH private key at test time
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	// Marshal to OpenSSH format
	rsaSSHKey, err := ssh.MarshalPrivateKey(rsaPriv, "")
	if err != nil {
		t.Fatalf("failed to marshal RSA key to SSH format: %v", err)
	}

	// Encode to PEM
	rsaPEM := pem.EncodeToMemory(rsaSSHKey)

	// Attempt to parse with ParsePrivateKey
	_, err = ParsePrivateKey(rsaPEM)
	if err == nil {
		t.Error("expected error for RSA key, got nil")
	}

	// Assert error mentions "only ed25519"
	if err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "only ed25519") {
			t.Errorf("expected error to mention 'only ed25519', got: %v", err)
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

	if !strings.Contains(err.Error(), "no PEM block") {
		t.Errorf("expected 'no PEM block' error, got: %v", err)
	}
}

func TestParsePublicKeyInvalidData(t *testing.T) {
	t.Parallel()

	_, err := ParsePublicKey([]byte("not valid key data"))
	if err == nil {
		t.Error("expected error for invalid key data")
	}

	if !strings.Contains(err.Error(), "no ed25519 public key found") {
		t.Errorf("expected 'no ed25519 public key found' error, got: %v", err)
	}
}
