package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
)

// SigningKey holds an RSA key pair for JWT signing
type SigningKey struct {
	ID         string
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// GenerateSigningKey creates a new RS256 signing key
func GenerateSigningKey() (*SigningKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}

	return &SigningKey{
		ID:         uuid.New().String(),
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}

// ExportPrivatePEM exports the private key as PEM
func (k *SigningKey) ExportPrivatePEM() (string, error) {
	privDER := x509.MarshalPKCS1PrivateKey(k.PrivateKey)
	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}
	return string(pem.EncodeToMemory(privBlock)), nil
}

// ExportPublicPEM exports the public key as PEM
func (k *SigningKey) ExportPublicPEM() (string, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(k.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshaling public key: %w", err)
	}
	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	}
	return string(pem.EncodeToMemory(pubBlock)), nil
}

// ImportFromPEM loads a signing key from PEM strings
func ImportFromPEM(id, privatePEM, publicPEM string) (*SigningKey, error) {
	privBlock, _ := pem.Decode([]byte(privatePEM))
	if privBlock == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	return &SigningKey{
		ID:         id,
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}

// JWK returns the public key as a JSON Web Key
func (k *SigningKey) JWK() jose.JSONWebKey {
	return jose.JSONWebKey{
		Key:       k.PublicKey,
		KeyID:     k.ID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
}

// SigningJWK returns the private key as a signing JSON Web Key
func (k *SigningKey) SigningJWK() jose.JSONWebKey {
	return jose.JSONWebKey{
		Key:       k.PrivateKey,
		KeyID:     k.ID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
}
