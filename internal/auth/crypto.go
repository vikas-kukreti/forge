package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 2
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// HashPassword hashes a password using Argon2id and returns a PHC string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	phc := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argon2Memory, argon2Time, argon2Threads, b64Salt, b64Hash)
	return phc, nil
}

// ComparePassword compares a PHC string password hash with a password.
func ComparePassword(password, hashPHC string) bool {
	parts := strings.Split(hashPHC, "$")
	if len(parts) != 6 {
		return false
	}
	if parts[1] != "argon2id" {
		return false
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil || version != argon2.Version {
		return false
	}

	var memory uint32
	var time uint32
	var threads uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	compareHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))
	return subtle.ConstantTimeCompare(hash, compareHash) == 1
}

// GenerateToken generates a cryptographically secure random token and its SHA256 hash.
func GenerateToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

// HashToken returns the SHA256 hash of a token.
func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

// GeneratePreviewID generates a 10-char lowercase Crockford base32 ID.
func GeneratePreviewID() (string, error) {
	b := make([]byte, 7) // 7 bytes is slightly more than 10 base32 chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.EncodeToString(b)
	encoded = strings.ToLower(encoded)
	if len(encoded) > 10 {
		return encoded[:10], nil
	}
	return encoded, nil
}
