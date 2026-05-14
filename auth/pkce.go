package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

type PKCEChallenge struct {
	Verifier  string
	Challenge string
}

func GeneratePKCE() (*PKCEChallenge, error) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return &PKCEChallenge{Verifier: verifier, Challenge: challenge}, nil
}
