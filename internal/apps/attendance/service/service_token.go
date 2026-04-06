package service

import (
	"context"
	"fmt"
	"time"

	goRedis "github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
)

func (s *AttendanceService) GenerateMagicLinkToken(orgID string, jwtSecret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"org_id": orgID,
		"scope":  "attendance_magic_link",
		"exp":    time.Now().AddDate(0, 1, 0).Unix(),
		"iat":    time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

func (s *AttendanceService) GenerateAppToken(orgID string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"org_id": orgID,
		"scope":  "attendance_magic_link",
		"exp":    time.Now().Add(365 * 24 * time.Hour).Unix(),
	})

	secret := s.kycService.Config.Security.JWTSecret
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	if err := s.redis.Set(ctx, fmt.Sprintf("attendance:magic_link:%s", orgID), tokenStr, 365*24*time.Hour).Err(); err != nil {
		return "", err
	}

	return tokenStr, nil
}

func (s *AttendanceService) GetActiveAppToken(orgID string) (string, error) {
	ctx := context.Background()
	token, err := s.redis.Get(ctx, fmt.Sprintf("attendance:magic_link:%s", orgID)).Result()
	if err == nil {
		return token, nil
	}
	if err != nil {
		if err == goRedis.Nil {
			return s.GenerateAppToken(orgID)
		}
		return s.GenerateAppToken(orgID)
	}
	return token, nil
}
