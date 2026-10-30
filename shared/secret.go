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
	"regexp"
	"strings"
)

const encryptedValuePrefix = "enc:v1:"

var namedKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// ErrSecretReentryRequired means a saved secret cannot be used on this installation and must be typed again.
var ErrSecretReentryRequired = errors.New("the saved key must be entered again")

// installationKeyName is the control panel's own key in ~/.owlcms. It is outside the install folder so copying that
// folder does not carry the key.
const installationKeyName = "controlpanel.aes"

// installationKeyPath returns ~/.owlcms/controlpanel.aes, after moving there a key left by an earlier control panel:
// ~/.owlcms/key (3.8.1 to 3.9.x), or the file ~/.owlcms itself (3.8.0). The key is unchanged, so saved secrets stay
// readable.
func installationKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	folder := filepath.Join(home, ".owlcms")
	keyPath := filepath.Join(folder, installationKeyName)
	if err := migrateInstallationKey(folder, keyPath); err != nil {
		return "", err
	}
	return keyPath, nil
}

func migrateInstallationKey(folder, keyPath string) error {
	if info, err := os.Stat(folder); err == nil && info.Mode().IsRegular() {
		// 3.8.0: the file ~/.owlcms is the key. Move it aside first, so it is never lost, then into the new folder.
		aside := folder + ".key-migration"
		if err := os.Rename(folder, aside); err != nil {
			return fmt.Errorf("moving legacy encryption key %s: %w", folder, err)
		}
		if err := os.MkdirAll(folder, 0o700); err != nil {
			_ = os.Rename(aside, folder)
			return fmt.Errorf("creating encryption key folder %s: %w", folder, err)
		}
		if err := os.Rename(aside, keyPath); err != nil {
			return fmt.Errorf("moving legacy encryption key to %s: %w", keyPath, err)
		}
		return nil
	}
	if _, err := os.Stat(keyPath); err == nil {
		return nil
	}
	oldPath := filepath.Join(folder, "key")
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Rename(oldPath, keyPath); err != nil {
			return fmt.Errorf("renaming encryption key %s to %s: %w", oldPath, keyPath, err)
		}
	}
	return nil
}

func loadInstallationKey(create bool) ([]byte, error) {
	keyPath, err := installationKeyPath()
	if err != nil {
		return nil, err
	}
	return loadKeyFile(keyPath, create)
}

// NamedKeyPath returns ~/.owlcms/<keyName>, for a key file kept apart from the control panel's own installation key.
func NamedKeyPath(keyName string) (string, error) {
	if !namedKeyPattern.MatchString(keyName) || keyName == "key" || keyName == installationKeyName {
		return "", fmt.Errorf("invalid key name %q: use letters, digits, '.', '_' or '-', not starting with '.', and not %q or \"key\"", keyName, installationKeyName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".owlcms", keyName), nil
}

// EncryptSecretWithNamedKey encrypts plain with the key in ~/.owlcms/<keyName>, creating it on first use.
// The program that decrypts the value reads the same file, for example OWLCMS with apiprotection.aes.
func EncryptSecretWithNamedKey(keyName, plain string) (string, error) {
	keyPath, err := NamedKeyPath(keyName)
	if err != nil {
		return "", err
	}
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	key, err := loadKeyFile(keyPath, true)
	if err != nil {
		return "", err
	}
	return sealSecret(key, plain)
}

func loadKeyFile(keyPath string, create bool) ([]byte, error) {
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid encryption key in %s", keyPath)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) || !create {
		return nil, fmt.Errorf("reading encryption key %s: %w", keyPath, err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating encryption key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating encryption key folder %s: %w", filepath.Dir(keyPath), err)
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating encryption key %s: %w", keyPath, err)
	}
	defer file.Close()
	if _, err := file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("writing encryption key %s: %w", keyPath, err)
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

// EncryptSecret encrypts plain with the installation key, creating ~/.owlcms/controlpanel.aes on first use.
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
	return sealSecret(key, plain)
}

func sealSecret(key []byte, plain string) (string, error) {
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
