package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID         string
	CustomerID string
	Email      string
}
type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) (*Manager, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("AUTH_SECRET is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Manager{secret: []byte(secret), ttl: ttl}, nil
}

func (m *Manager) Issue(user User) (string, error) {
	expires := time.Now().Add(m.ttl).Unix()
	payload := strings.Join([]string{user.ID, user.CustomerID, user.Email, strconv.FormatInt(expires, 10)}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + m.sign(encoded), nil
}

func (m *Manager) Parse(token string) (User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(m.sign(parts[0]))) {
		return User{}, errors.New("invalid token")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return User{}, errors.New("invalid token")
	}
	fields := strings.Split(string(data), "|")
	if len(fields) != 4 {
		return User{}, errors.New("invalid token")
	}
	expires, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || time.Now().Unix() >= expires {
		return User{}, errors.New("token expired")
	}
	return User{ID: fields[0], CustomerID: fields[1], Email: fields[2]}, nil
}

func (m *Manager) sign(value string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (u User) String() string { return fmt.Sprintf("%s <%s>", u.ID, u.Email) }
