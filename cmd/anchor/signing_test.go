package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrew-d/anchor/internal/signing"
)

// TestRunKeygen verifies that runKeygen produces valid keypair files with correct permissions.
// Tests module-signing.AC1.1
func TestRunKeygen(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outputBase := filepath.Join(tmpDir, "test")

	exitCode := runKeygen([]string{"-o", outputBase})

	if exitCode != 0 {
		t.Fatalf("runKeygen returned %d, expected 0", exitCode)
	}

	keyPath := outputBase + ".key"
	pubPath := outputBase + ".pub"

	// Verify both files exist
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("private key file not found: %v", err)
	}
	if _, err := os.Stat(pubPath); err != nil {
		t.Fatalf("public key file not found: %v", err)
	}

	// Verify file permissions
	keyInfo, _ := os.Stat(keyPath)
	if keyInfo.Mode()&0o777 != 0o600 {
		t.Errorf("private key mode is %o, expected 0600", keyInfo.Mode()&0o777)
	}

	pubInfo, _ := os.Stat(pubPath)
	if pubInfo.Mode()&0o777 != 0o644 {
		t.Errorf("public key mode is %o, expected 0644", pubInfo.Mode()&0o777)
	}

	// Verify files contain valid PEM
	keyData, _ := os.ReadFile(keyPath)
	privKey, err := signing.ParsePrivateKey(keyData)
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
	if privKey == nil {
		t.Fatal("parsed private key is nil")
	}

	pubData, _ := os.ReadFile(pubPath)
	pubKey, err := signing.ParsePublicKey(pubData)
	if err != nil {
		t.Fatalf("failed to parse public key: %v", err)
	}
	if pubKey == nil {
		t.Fatal("parsed public key is nil")
	}
}

// TestRunKeygenUnwritablePath verifies that runKeygen returns error when output path is not writable.
// Tests module-signing.AC1.4
func TestRunKeygenUnwritablePath(t *testing.T) {
	t.Parallel()

	exitCode := runKeygen([]string{"-o", "/nonexistent/dir/key"})

	if exitCode != 1 {
		t.Fatalf("runKeygen returned %d, expected 1", exitCode)
	}
}

// TestRunSignProducesSigFile verifies that runSign produces a valid signature file.
// Tests module-signing.AC2.1
func TestRunSignProducesSigFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Generate keypair
	keyBase := filepath.Join(tmpDir, "key")
	exitCode := runKeygen([]string{"-o", keyBase})
	if exitCode != 0 {
		t.Fatalf("runKeygen failed with exit code %d", exitCode)
	}

	keyPath := keyBase + ".key"
	pubPath := keyBase + ".pub"

	// Create a module file with known content
	moduleContent := []byte("#!/bin/bash\necho 'test module'\n")
	modulePath := filepath.Join(tmpDir, "test_module.sh")
	if err := os.WriteFile(modulePath, moduleContent, 0644); err != nil {
		t.Fatalf("failed to write module file: %v", err)
	}

	// Sign the module
	exitCode = runSign([]string{"-k", keyPath, modulePath})
	if exitCode != 0 {
		t.Fatalf("runSign returned %d, expected 0", exitCode)
	}

	// Verify .sig file exists and contains valid hex
	sigPath := modulePath + ".sig"
	if _, err := os.Stat(sigPath); err != nil {
		t.Fatalf("signature file not found: %v", err)
	}

	sigData, _ := os.ReadFile(sigPath)
	sigBytes, err := hex.DecodeString(string(sigData))
	if err != nil {
		t.Fatalf("failed to decode signature hex: %v", err)
	}

	// Verify signature is 64 bytes (ed25519 signature size)
	if len(sigBytes) != 64 {
		t.Errorf("signature size is %d, expected 64", len(sigBytes))
	}

	// Verify signature is valid
	pubData, _ := os.ReadFile(pubPath)
	pubKey, _ := signing.ParsePublicKey(pubData)
	if !signing.Verify(pubKey, moduleContent, sigBytes) {
		t.Error("signature verification failed")
	}
}

// TestRunSignMultipleModules verifies that runSign can sign multiple modules in one invocation.
// Tests module-signing.AC2.4
func TestRunSignMultipleModules(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Generate keypair
	keyBase := filepath.Join(tmpDir, "key")
	exitCode := runKeygen([]string{"-o", keyBase})
	if exitCode != 0 {
		t.Fatalf("runKeygen failed with exit code %d", exitCode)
	}

	keyPath := keyBase + ".key"
	pubPath := keyBase + ".pub"

	// Create two module files
	module1Content := []byte("#!/bin/bash\necho 'module 1'\n")
	module1Path := filepath.Join(tmpDir, "module1.sh")
	if err := os.WriteFile(module1Path, module1Content, 0644); err != nil {
		t.Fatalf("failed to write module 1: %v", err)
	}

	module2Content := []byte("#!/bin/bash\necho 'module 2'\n")
	module2Path := filepath.Join(tmpDir, "module2.sh")
	if err := os.WriteFile(module2Path, module2Content, 0644); err != nil {
		t.Fatalf("failed to write module 2: %v", err)
	}

	// Sign both modules
	exitCode = runSign([]string{"-k", keyPath, module1Path, module2Path})
	if exitCode != 0 {
		t.Fatalf("runSign returned %d, expected 0", exitCode)
	}

	// Verify both .sig files exist and contain valid signatures
	sig1Path := module1Path + ".sig"
	if _, err := os.Stat(sig1Path); err != nil {
		t.Fatalf("signature file 1 not found: %v", err)
	}

	sig2Path := module2Path + ".sig"
	if _, err := os.Stat(sig2Path); err != nil {
		t.Fatalf("signature file 2 not found: %v", err)
	}

	// Verify both signatures are valid
	pubData, _ := os.ReadFile(pubPath)
	pubKey, _ := signing.ParsePublicKey(pubData)

	sig1Data, _ := os.ReadFile(sig1Path)
	sig1Bytes, _ := hex.DecodeString(string(sig1Data))
	if !signing.Verify(pubKey, module1Content, sig1Bytes) {
		t.Error("signature 1 verification failed")
	}

	sig2Data, _ := os.ReadFile(sig2Path)
	sig2Bytes, _ := hex.DecodeString(string(sig2Data))
	if !signing.Verify(pubKey, module2Content, sig2Bytes) {
		t.Error("signature 2 verification failed")
	}
}

// TestRunSignMissingKeyFile verifies that runSign returns error when key file is missing.
// Tests module-signing.AC2.6
func TestRunSignMissingKeyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modulePath := filepath.Join(tmpDir, "module.sh")

	exitCode := runSign([]string{"-k", "/nonexistent/key", modulePath})

	if exitCode != 1 {
		t.Fatalf("runSign returned %d, expected 1", exitCode)
	}
}

// TestRunSignNoKeyFlag verifies that runSign returns error when -k flag is not provided.
func TestRunSignNoKeyFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	modulePath := filepath.Join(tmpDir, "module.sh")

	exitCode := runSign([]string{modulePath})

	if exitCode != 1 {
		t.Fatalf("runSign returned %d, expected 1", exitCode)
	}
}
