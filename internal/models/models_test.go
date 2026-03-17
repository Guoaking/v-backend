package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKYCRequestJSON(t *testing.T) {
	req := KYCRequest{
		ID:           "123",
		UserID:       "u1",
		IDCard:       "secret", // Should be hidden
		Status:       "pending",
		CreatedAt:    time.Now(),
	}

	b, err := json.Marshal(req)
	assert.NoError(t, err)
	
	s := string(b)
	assert.Contains(t, s, `"id":"123"`)
	assert.NotContains(t, s, "IDCard") // Check json:"-"
	assert.NotContains(t, s, "secret")
}

func TestUserJSON(t *testing.T) {
	u := User{
		ID:       "u1",
		Password: "hashed_password",
		Role:     "admin",
	}
	
	b, err := json.Marshal(u)
	assert.NoError(t, err)
	
	s := string(b)
	assert.Contains(t, s, `"role":"admin"`)
	assert.NotContains(t, s, "password")
}
