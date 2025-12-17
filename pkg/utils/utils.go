package utils

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	mrand "math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"
)

/**
@description
@date: 11/20 22:38
@author Gk
**/

func GenerateRandomNumbers(s int) int {
	// 设置随机数种子
	mrand.Seed(time.Now().UnixNano())

	// 生成1-10之间的随机数
	return mrand.Intn(s) + 1
}

func GenerateRandomBool() bool {
	// 设置随机数种子
	mrand.Seed(time.Now().UnixNano())
	return mrand.Intn(2) == 1
}

// GenerateID 生成唯一ID
func GenerateID() string {
	return uuid.New().String()
}

// GenerateAPIKey 生成API密钥
func GenerateAPIKey() string {
	return "kyc_" + GenerateID()
}

// GenerateAPISecret 生成API密钥
func GenerateAPISecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// GenerateSecureRandomNumbers 生成安全的随机数字
func GenerateSecureRandomNumbers(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

// Contains 检查字符串是否在切片中
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ToJSONString 将对象转换为JSON字符串
func ToJSONString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseJSONStringArray 解析JSON字符串数组
func ParseJSONStringArray(jsonStr string) []string {
	var result []string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return []string{}
	}
	return result
}

// FormatTime 格式化时间
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func FormatTimeUnix(t time.Time) string {
	return strconv.Itoa(int(t.Unix()))
}
