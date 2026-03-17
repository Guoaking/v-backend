package utils

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRandomNumbers(t *testing.T) {
	n := GenerateRandomNumbers(10)
	assert.GreaterOrEqual(t, n, 1)
	assert.LessOrEqual(t, n, 10)
}

func TestGenerateRandomBool(t *testing.T) {
	// Simple sanity check
	_ = GenerateRandomBool()
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	assert.NotEmpty(t, id1)
	assert.NotEqual(t, id1, id2)
}

func TestGenerateAPIKey(t *testing.T) {
	key := GenerateAPIKey()
	assert.True(t, strings.HasPrefix(key, "kyc_"))
	assert.Len(t, key, 40) // kyc_ + 36 uuid
}

func TestGenerateAPISecret(t *testing.T) {
	secret := GenerateAPISecret()
	assert.NotEmpty(t, secret)
	assert.Greater(t, len(secret), 30)
}

func TestGenerateSecureRandomNumbers(t *testing.T) {
	n := GenerateSecureRandomNumbers(100)
	assert.GreaterOrEqual(t, n, 0)
	assert.Less(t, n, 100)
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}
	assert.True(t, Contains(slice, "b"))
	assert.False(t, Contains(slice, "d"))
}

func TestToJSONString(t *testing.T) {
	data := map[string]string{"foo": "bar"}
	jsonStr := ToJSONString(data)
	assert.Equal(t, `{"foo":"bar"}`, jsonStr)

	// Test invalid input (channel cannot be marshaled)
	invalid := make(chan int)
	assert.Equal(t, "[]", ToJSONString(invalid))
}

func TestParseJSONStringArray(t *testing.T) {
	jsonStr := `["a", "b"]`
	arr := ParseJSONStringArray(jsonStr)
	assert.Equal(t, []string{"a", "b"}, arr)

	// Test invalid JSON
	assert.Empty(t, ParseJSONStringArray(`invalid`))
}

func TestFormatTime(t *testing.T) {
	now := time.Now()
	str := FormatTime(now)
	assert.Equal(t, now.Format("2006-01-02 15:04:05"), str)
}

func TestFormatTimeUnix(t *testing.T) {
	now := time.Now()
	str := FormatTimeUnix(now)
	assert.NotEmpty(t, str)
}
