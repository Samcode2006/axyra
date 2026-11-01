package provision

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecrets(t *testing.T) {
	c, err := NewCipher(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"password":"secret"}`)
	encrypted := seal(c, "tenant_a", raw)
	if bytes.Contains(encrypted, raw) {
		t.Fatal("plaintext stored")
	}
	restored, err := unseal(c, "tenant_a", encrypted)
	if err != nil || !bytes.Equal(raw, restored) {
		t.Fatal("round trip failed")
	}
	if _, err = unseal(c, "tenant_b", encrypted); err == nil {
		t.Fatal("cross tenant decrypt accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = unseal(c, "tenant_a", encrypted); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err = NewCipher("short"); err == nil {
		t.Fatal("short key accepted")
	}
}
