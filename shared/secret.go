package shared

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const encryptedValuePrefix = "enc:v1:"

// ErrSecretReentryRequired means a saved secret cannot be used on this installation and must be typed again.
var ErrSecretReentryRequired = errors.New("the saved key must be entered again")

// installationKeyPath is outside the install folder so copying that folder does not carry the key.
// Control panel 3.8.0 stored the key as the file ~/.owlcms; that file is still used when present.
func installationKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	legacyPath := filepath.Join(home, ".owlcms")
	if info, err := os.Stat(legacyPath); err == nil && info.Mode().IsRegular() {
		return legacyPath, nil
	}
	return filepath.Join(legacyPath, "key"), nil
}

func loadInstallationKey(create bool) ([]byte, error) {
	keyPath, err := installationKeyPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid installation key in %s", keyPath)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) || !create {
		return nil, fmt.Errorf("reading installation key %s: %w", keyPath, err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating installation key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating installation key folder %s: %w", filepath.Dir(keyPath), err)
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating installation key %s: %w", keyPath, err)
	}
	defer file.Close()
	if _, err := file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("writing installation key %s: %w", keyPath, err)
	}
	return key, nil
}

func installationKeyFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:4])
}

func newInstallationCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// IsSecretSet reports whether a saved key property holds a key: an empty key is saved as an empty value, never encrypted.
func IsSecretSet(value string) bool {
	return strings.TrimSpace(value) != ""
}

// EncryptSecret encrypts plain with the installation key, creating ~/.owlcms/key on first use.
// A blank key yields an empty value.
func EncryptSecret(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	key, err := loadInstallationKey(true)
	if err != nil {
		return "", err
	}
	gcm, err := newInstallationCipher(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encryptedValuePrefix + installationKeyFingerprint(key) + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret returns values without the encryption prefix unchanged, so hand-edited plain values still work.
func DecryptSecret(value string) (string, error) {
	if !IsSecretSet(value) {
		return "", nil
	}
	payload, ok := strings.CutPrefix(strings.TrimSpace(value), encryptedValuePrefix)
	if !ok {
		return value, nil
	}
	fingerprint, encoded, ok := strings.Cut(payload, ":")
	if !ok {
		return "", fmt.Errorf("%w: the saved value is invalid", ErrSecretReentryRequired)
	}
	key, err := loadInstallationKey(false)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: it was encrypted on another computer or the installation key was removed", ErrSecretReentryRequired)
	}
	if err != nil {
		return "", err
	}
	if fingerprint != installationKeyFingerprint(key) {
		return "", fmt.Errorf("%w: it was encrypted on another computer", ErrSecretReentryRequired)
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: the saved value is invalid", ErrSecretReentryRequired)
	}
	gcm, err := newInstallationCipher(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("%w: the saved value is invalid", ErrSecretReentryRequired)
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("%w: the saved value is invalid", ErrSecretReentryRequired)
	}
	return string(plain), nil
}
