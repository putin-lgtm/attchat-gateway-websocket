package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/attchat/attchat-gateway/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken     = errors.New("invalid token")
	ErrExpiredToken     = errors.New("token expired")
	ErrInvalidClaims    = errors.New("invalid claims")
	ErrMissingUserID    = errors.New("missing user_id in token")
)

// Claims represents JWT claims for ATTChat
type Claims struct {
	jwt.RegisteredClaims
	UserID   string   `json:"user_id"`
	BrandID  string   `json:"brand_id"`
	Role     string   `json:"role"`
	Rooms    []string `json:"rooms,omitempty"`
	Type     string   `json:"type"` // "cskh" or "customer"
}

// JWTValidator validates JWT tokens
type JWTValidator struct {
	secretKey      []byte
	validateExp    bool
	allowedIssuers []string
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(cfg config.JWTConfig) *JWTValidator {
	return &JWTValidator{
		secretKey:      []byte(cfg.SecretKey),
		validateExp:    cfg.ValidateExp,
		allowedIssuers: cfg.AllowedIssuers,
	}
}

// Validate validates a JWT token and returns claims
func (v *JWTValidator) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.secretKey, nil
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
	if claims.UserID == "" {
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secretKey))
}

