package services

import (
	"bytes"
	"testing"
)

func TestKeyvaultRoundtrip(t *testing.T) {
	// 32-byte hex key
	if err := InitKeyVault("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer func() { defaultVault = nil }()

	plain := []byte("sk-secret-9999")
	ct, nonce, ver, err := Vault().Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(ct, plain) {
		t.Fatalf("ciphertext contains plaintext")
	}
	got, err := Vault().Decrypt(ct, nonce, ver)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip: got %q want %q", got, plain)
	}
}

func TestKeyvaultVersionMismatch(t *testing.T) {
	if err := InitKeyVault("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer func() { defaultVault = nil }()

	ct, nonce, _, err := Vault().Encrypt([]byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Vault().Decrypt(ct, nonce, 99); err == nil {
		t.Fatal("expected error decrypting under unknown version")
	}
}

func TestKeyvaultRotation(t *testing.T) {
	// Register an old v1 key and a current key (which auto-numbers as v2).
	if err := InitKeyVault(
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		map[int]string{1: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
	); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer func() { defaultVault = nil }()

	plain := []byte("rotate me")
	ct, nonce, ver, err := Vault().Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if ver != 2 {
		t.Fatalf("expected new writes to use v2, got %d", ver)
	}
	got, err := Vault().Decrypt(ct, nonce, ver)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch")
	}
}
