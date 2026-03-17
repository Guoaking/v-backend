package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewEncryptor(t *testing.T) {
	// Valid key
	key := "12345678901234567890123456789012"
	enc, err := NewEncryptor(key)
	assert.NoError(t, err)
	assert.NotNil(t, enc)

	// Invalid key length
	_, err = NewEncryptor("short")
	assert.Error(t, err)
}

func TestEncryptDecrypt(t *testing.T) {
	key := "12345678901234567890123456789012"
	enc, _ := NewEncryptor(key)

	plaintext := "secret message"
	
	// Encrypt
	ciphertext, err := enc.Encrypt(plaintext)
	assert.NoError(t, err)
	assert.NotEmpty(t, ciphertext)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := enc.Decrypt(ciphertext)
	assert.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestDecryptInvalid(t *testing.T) {
	key := "12345678901234567890123456789012"
	enc, _ := NewEncryptor(key)

	// Invalid base64
	_, err := enc.Decrypt("invalid-base64")
	assert.Error(t, err)

	// Valid base64 but too short
	_, err = enc.Decrypt("MTI=") // "12" base64 encoded
	assert.Error(t, err)
}

func TestHashIDCard(t *testing.T) {
	hash1 := HashIDCard("123456789")
	hash2 := HashIDCard("123456789")
	hash3 := HashIDCard("987654321")

	assert.Equal(t, hash1, hash2)
	assert.NotEqual(t, hash1, hash3)
	assert.Len(t, hash1, 16) // 8 bytes hex encoded = 16 chars
}

func TestHashString(t *testing.T) {
	h1, _ := HashString("secret")
	h2, _ := HashString("secret")
	assert.Equal(t, h1, h2)
}
