package provision

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func NewCipher(key string) (cipher.AEAD, error) {
	raw, err := hex.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("PROVISIONING_KEY must contain 64 hex characters")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func seal(c cipher.AEAD, tenant string, plain []byte) []byte {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return c.Seal(nonce, nonce, plain, []byte(tenant))
}
func unseal(c cipher.AEAD, tenant string, encrypted []byte) ([]byte, error) {
	if len(encrypted) < c.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted credentials")
	}
	return c.Open(nil, encrypted[:c.NonceSize()], encrypted[c.NonceSize():], []byte(tenant))
}
func password() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
