package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

type Encryptor struct {
	key []byte
}

func NewEncryptor(key string) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("加密密钥必须是32字节")
	}
	return &Encryptor{key: []byte(key)}, nil
}

// Encrypt 加密数据
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密数据
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	if len(data) < gcm.NonceSize() {
		return "", errors.New("密文太短")
	}
	
	nonce, ciphertextData := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextData, nil)
	if err != nil {
		return "", err
	}
	
	return string(plaintext), nil
}

// HashIDCard 身份证号哈希（用于索引）
func HashIDCard(id string) string {
	// 使用SHA256哈希，只存储前8位用于索引
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:8])
}

// HashString 字符串哈希（用于API密钥存储）
func HashString(input string) (string, error) {
	// 使用SHA256哈希整个字符串
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:]), nil
}