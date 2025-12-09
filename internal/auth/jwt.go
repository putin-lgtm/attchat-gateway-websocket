package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("token expired")
	ErrInvalidClaims = errors.New("invalid claims")
	ErrMissingUserID = errors.New("missing user_id in token")
)

// Claims represents JWT claims for ATTChat
type Claims struct {
	jwt.RegisteredClaims
	UserID       uint     `json:"user_id"`
	Username     string   `json:"username"`
	RoleID       uint     `json:"role_id"`
	TokenVersion int      `json:"token_version"`
	BrandID      string   `json:"brand_id,omitempty"`
	Role         string   `json:"role,omitempty"`
	Rooms        []string `json:"rooms,omitempty"`
	Type         string   `json:"type,omitempty"` // "cskh" or "customer"
}

// JWTValidator validates JWT tokens
type JWTValidator struct {
	publicKey      *rsa.PublicKey
	validateExp    bool
	allowedIssuers []string
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(cfg config.JWTConfig) (*JWTValidator, error) {
	if cfg.PublicKeyPEM == "" {
		return nil, fmt.Errorf("jwt.public_key is required (RS256 only)")
	}
	pk, err := parseRSAPublicKey(cfg.PublicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT public key: %w", err)
	}
	return &JWTValidator{
		publicKey:      pk,
		validateExp:    cfg.ValidateExp,
		allowedIssuers: cfg.AllowedIssuers,
	}, nil
}

// Validate validates a JWT token and returns claims
func (v *JWTValidator) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.publicKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidClaims
	}

	// Validate user_id
	if claims.UserID == 0 {
		return nil, ErrMissingUserID
	}

	// Validate expiration
	if v.validateExp {
		if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
			return nil, ErrExpiredToken
		}
	}

	// Validate issuer
	if len(v.allowedIssuers) > 0 {
		issuerValid := false
		for _, iss := range v.allowedIssuers {
			if claims.Issuer == iss {
				issuerValid = true
				break
			}
		}
		if !issuerValid {
			return nil, fmt.Errorf("%w: invalid issuer", ErrInvalidClaims)
		}
	}

	return claims, nil
}

// GenerateToken generates a JWT token (for testing purposes)
func GenerateToken(secretKey string, claims *Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// This helper now expects secretKey to be a PEM-encoded RSA private key string.
	block, _ := pem.Decode([]byte(normalizePEM(secretKey)))
	if block == nil {
		return "", fmt.Errorf("failed to decode private key PEM")
	}
	pk, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	return token.SignedString(pk)
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	pemStr = normalizePEM(pemStr)
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM")
	}
	// Try PKCS1 (BEGIN RSA PUBLIC KEY)
	if pkcs1, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pkcs1, nil
	}
	// Try PKIX (BEGIN PUBLIC KEY)
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if k, ok := pub.(*rsa.PublicKey); ok {
			return k, nil
		}
		return nil, fmt.Errorf("not RSA public key")
	}
	// Try cert
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if k, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return k, nil
		}
		return nil, fmt.Errorf("cert is not RSA")
	}
	return nil, fmt.Errorf("parse public key failed")
}

func normalizePEM(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "\uFEFF") // strip BOM if present
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		s = strings.Trim(s, "\"")
	}
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
