package apiv1

import "testing"

func TestKeyCryptoRoundTrip(t *testing.T) {
	if err := InitKeyCipher("11111111111111111111111111111111111111111111111111111111111111ab"); err != nil {
		t.Fatalf("InitKeyCipher: %v", err)
	}
	plaintext := "bt_live_" + "abcdef0123456789abcdef0123456789"

	enc, err := encryptSecret(plaintext)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	if enc == plaintext {
		t.Fatal("ciphertext equals plaintext — not encrypted")
	}
	if got := decryptSecret(enc); got != plaintext {
		t.Fatalf("decryptSecret round-trip = %q, want %q", got, plaintext)
	}

	// Two encryptions of the same key differ (random nonce) but both decrypt.
	enc2, _ := encryptSecret(plaintext)
	if enc2 == enc {
		t.Fatal("nonce reuse: identical ciphertext for repeated encryption")
	}
	if decryptSecret(enc2) != plaintext {
		t.Fatal("second ciphertext failed to decrypt")
	}

	// A different key cannot decrypt (returns "" rather than leaking).
	if err := InitKeyCipher("22222222222222222222222222222222222222222222222222222222222222cd"); err != nil {
		t.Fatalf("InitKeyCipher (2): %v", err)
	}
	if got := decryptSecret(enc); got != "" {
		t.Fatalf("decrypt with wrong key returned %q, want \"\"", got)
	}
}

func TestInitKeyCipherRejectsBadKey(t *testing.T) {
	if err := InitKeyCipher("not-hex-!!!"); err == nil {
		t.Fatal("expected error for non-hex key")
	}
	if err := InitKeyCipher("abcd"); err == nil {
		t.Fatal("expected error for short key")
	}
}
