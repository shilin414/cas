package aimodel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

type credentialCipher struct{ aead cipher.AEAD }

// Derive a distinct AES key from TokenEncryptionKey. Both the purpose and row ID
// are authenticated, so ciphertext cannot be swapped with OAuth tokens or another connection.
func newCredentialCipher(master string) (*credentialCipher, error) {
	if strings.TrimSpace(master) == "" {
		return nil, ErrUnavailable
	}
	kdf := hmac.New(sha256.New, []byte(master))
	kdf.Write([]byte("xiaoan-platform/ai-connection/credential/v1"))
	block, err := aes.NewCipher(kdf.Sum(nil))
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &credentialCipher{aead: aead}, nil
}
func (c *credentialCipher) encrypt(id, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrUnavailable
	}
	out := c.aead.Seal(nonce, nonce, []byte(plain), []byte("ai-credential:v1:"+id))
	return "v1:" + base64.RawStdEncoding.EncodeToString(out), nil
}
func (c *credentialCipher) decrypt(id, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if !strings.HasPrefix(encoded, "v1:") {
		return "", ErrUnavailable
	}
	buf, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil || len(buf) < c.aead.NonceSize()+c.aead.Overhead() {
		return "", ErrUnavailable
	}
	n := c.aead.NonceSize()
	out, err := c.aead.Open(nil, buf[:n], buf[n:], []byte("ai-credential:v1:"+id))
	if err != nil {
		return "", ErrUnavailable
	}
	return string(out), nil
}
