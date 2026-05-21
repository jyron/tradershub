package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

// Keyvault encrypts and decrypts submitter LLM API keys with AES-256-GCM.
//
// The active master key writes new rows under `activeVersion`. Older versions
// remain in the `keys` map so that, during rotation, rows encrypted under an
// older version can still be decrypted until they're rewritten.
type Keyvault struct {
	mu            sync.RWMutex
	keys          map[int]cipher.AEAD
	activeVersion int
}

var defaultVault *Keyvault

// InitKeyVault is called once at server startup with the currently-active
// 32-byte hex key plus any older versions still needed for in-flight rows.
// Passing an empty currentKey returns nil (operator opted into running without
// the vault — submission endpoints should then fail closed, not silently).
func InitKeyVault(currentKey string, versions map[int]string) error {
	if currentKey == "" {
		defaultVault = nil
		return errors.New("BOTTRADE_MASTER_KEY is not set")
	}
	v := &Keyvault{keys: map[int]cipher.AEAD{}}

	addKey := func(version int, hexKey string) error {
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			return fmt.Errorf("key v%d: invalid hex: %w", version, err)
		}
		if len(raw) != 32 {
			return fmt.Errorf("key v%d: must be 32 bytes (got %d)", version, len(raw))
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return fmt.Errorf("key v%d: %w", version, err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return fmt.Errorf("key v%d: %w", version, err)
		}
		v.keys[version] = gcm
		return nil
	}

	currentVersion := 1
	for ver := range versions {
		if ver >= currentVersion {
			currentVersion = ver + 1
		}
	}
	if err := addKey(currentVersion, currentKey); err != nil {
		return err
	}
	v.activeVersion = currentVersion
	for ver, hk := range versions {
		if err := addKey(ver, hk); err != nil {
			return err
		}
	}

	defaultVault = v
	return nil
}

// Vault returns the process-wide vault. nil means submission/decryption flows
// must refuse the request.
func Vault() *Keyvault { return defaultVault }

func (v *Keyvault) Encrypt(plaintext []byte) (ciphertext, nonce []byte, version int, err error) {
	if v == nil {
		return nil, nil, 0, errors.New("keyvault not initialized")
	}
	v.mu.RLock()
	aead := v.keys[v.activeVersion]
	version = v.activeVersion
	v.mu.RUnlock()

	nonce = make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, 0, err
	}
	ciphertext = aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, version, nil
}

func (v *Keyvault) Decrypt(ciphertext, nonce []byte, version int) ([]byte, error) {
	if v == nil {
		return nil, errors.New("keyvault not initialized")
	}
	v.mu.RLock()
	aead, ok := v.keys[version]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no key registered for version %d (rotation in flight?)", version)
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}
