package passwords

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// The Bitwarden client protocol: a key from the master password, a random
// account key wrapped by it, and items encrypted as "2.iv|data|mac" strings
// (AES-256-CBC with HMAC-SHA256, encrypt-then-MAC).

// vwKDF is the server's answer about how to derive the master key.
type vwKDF struct {
	Kdf            int `json:"kdf"` // 0 PBKDF2-SHA256, 1 Argon2id
	KdfIterations  int `json:"kdfIterations"`
	KdfMemory      int `json:"kdfMemory"`
	KdfParallelism int `json:"kdfParallelism"`
}

// symKey is a pair of 256-bit keys, one to encrypt and one to authenticate.
type symKey struct{ enc, mac []byte }

func newSymKey(raw []byte) (symKey, error) {
	if len(raw) != 64 {
		return symKey{}, fmt.Errorf("неожиданная длина ключа: %d", len(raw))
	}
	return symKey{enc: raw[:32], mac: raw[32:]}, nil
}

func deriveMasterKey(password []byte, email string, k vwKDF) ([]byte, error) {
	switch k.Kdf {
	case 0:
		if k.KdfIterations < 1 {
			return nil, errors.New("сервер вернул неверные параметры PBKDF2")
		}
		return pbkdf2.Key(sha256.New, string(password), []byte(email), k.KdfIterations, 32)
	case 1:
		if k.KdfIterations < 1 || k.KdfMemory < 1 || k.KdfParallelism < 1 || k.KdfParallelism > 255 {
			return nil, errors.New("сервер вернул неверные параметры Argon2")
		}
		salt := sha256.Sum256([]byte(email))
		return argon2.IDKey(password, salt[:], uint32(k.KdfIterations), uint32(k.KdfMemory)*1024, uint8(k.KdfParallelism), 32), nil
	}
	return nil, fmt.Errorf("неизвестный алгоритм ключа: %d", k.Kdf)
}

// masterPasswordHash proves knowledge of the password without revealing the
// key: one more PBKDF2 round over the master key, salted with the password.
func masterPasswordHash(master, password []byte) string {
	out, err := pbkdf2.Key(sha256.New, string(master), password, 1, 32)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(out)
}

func stretchMasterKey(master []byte) (symKey, error) {
	enc, err := hkdf.Expand(sha256.New, master, "enc", 32)
	if err != nil {
		return symKey{}, err
	}
	mac, err := hkdf.Expand(sha256.New, master, "mac", 32)
	if err != nil {
		return symKey{}, err
	}
	return symKey{enc: enc, mac: mac}, nil
}

// decryptString opens a type 2 string. The tag is checked before any decryption.
func decryptString(s string, key symKey) ([]byte, error) {
	kind, rest, ok := strings.Cut(s, ".")
	if !ok || kind != "2" {
		return nil, fmt.Errorf("неподдерживаемый формат шифрования %q", kind)
	}
	parts := strings.Split(rest, "|")
	if len(parts) != 3 {
		return nil, errors.New("повреждённая запись")
	}
	iv, err1 := base64.StdEncoding.DecodeString(parts[0])
	data, err2 := base64.StdEncoding.DecodeString(parts[1])
	tag, err3 := base64.StdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, errors.New("повреждённая запись")
	}
	mac := hmac.New(sha256.New, key.mac)
	mac.Write(iv)
	mac.Write(data)
	if !hmac.Equal(mac.Sum(nil), tag) {
		return nil, errors.New("не сошлась подпись записи")
	}
	block, err := aes.NewCipher(key.enc)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("повреждённая запись")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(out) {
		return nil, errors.New("повреждённая запись")
	}
	return out[:len(out)-pad], nil
}

// decryptOrgKey unwraps an organization key with the account's RSA key:
// type 4 is OAEP with SHA-1, type 3 with SHA-256.
func decryptOrgKey(s string, privateDER []byte) ([]byte, error) {
	kind, data, ok := strings.Cut(s, ".")
	if !ok {
		return nil, errors.New("повреждённый ключ организации")
	}
	var h hash.Hash
	switch kind {
	case "3":
		h = sha256.New()
	case "4":
		h = sha1.New()
	default:
		return nil, fmt.Errorf("неподдерживаемый ключ организации %q", kind)
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(privateDER)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("ключ учётной записи — не RSA")
	}
	return rsa.DecryptOAEP(h, rand.Reader, key, raw, nil)
}

// totpCode computes the current one-time code (RFC 6238) from what the item
// stores: a bare base32 secret or an otpauth:// URI.
func totpCode(stored string, now time.Time) (string, error) {
	secret, digits, period := strings.TrimSpace(stored), 6, int64(30)
	var newHash func() hash.Hash = sha1.New
	if strings.HasPrefix(strings.ToLower(secret), "steam://") {
		return "", errors.New("коды Steam Guard не поддерживаются")
	}
	if strings.HasPrefix(strings.ToLower(secret), "otpauth://") {
		u, err := url.Parse(secret)
		if err != nil {
			return "", errors.New("неверная ссылка otpauth")
		}
		q := u.Query()
		secret = q.Get("secret")
		if d, err := strconv.Atoi(q.Get("digits")); err == nil && d >= 6 && d <= 10 {
			digits = d
		}
		if p, err := strconv.ParseInt(q.Get("period"), 10, 64); err == nil && p > 0 {
			period = p
		}
		switch strings.ToUpper(q.Get("algorithm")) {
		case "", "SHA1":
		case "SHA256":
			newHash = sha256.New
		case "SHA512":
			newHash = sha512.New
		default:
			return "", errors.New("неизвестный алгоритм кода")
		}
	}
	secret = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(secret))
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(raw) == 0 {
		return "", errors.New("неверный секрет кода")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/period))
	mac := hmac.New(newHash, raw)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	at := sum[len(sum)-1] & 0x0f
	value := uint64(binary.BigEndian.Uint32(sum[at:at+4]) & 0x7fffffff)
	mod := uint64(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod), nil
}
