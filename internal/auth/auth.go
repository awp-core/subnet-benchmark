package auth

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// VerifySignature verifies an Ethereum signature and returns the recovered address.
// message is the original message; signatureHex is the hex-encoded signature (with 0x prefix).
func VerifySignature(message string, signatureHex string) (common.Address, error) {
	sigBytes, err := hexDecode(signatureHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBytes) != 65 {
		return common.Address{}, fmt.Errorf("invalid signature length: %d", len(sigBytes))
	}

	// Ethereum personal_sign format
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256([]byte(prefixed))

	// Normalize V value: 27/28 → 0/1
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	pubKey, err := crypto.SigToPub(hash, sigBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("recover public key: %w", err)
	}

	return crypto.PubkeyToAddress(*pubKey), nil
}

// BuildSignMessage constructs the message to be signed.
func BuildSignMessage(method, path, timestamp, bodyHash string) string {
	return method + path + timestamp + bodyHash
}

// HashBody computes the SHA256 hash of the request body (hex encoded).
func HashBody(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// SignMessage signs a message with a private key (for testing).
func SignMessage(privKey *ecdsa.PrivateKey, message string) ([]byte, error) {
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256([]byte(prefixed))
	return crypto.Sign(hash, privKey)
}

// AddressFromPrivateKey derives the Ethereum address from a private key.
func AddressFromPrivateKey(privKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privKey.PublicKey)
}

func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	return hex.DecodeString(s)
}

// ChecksumAddress returns the EIP-55 checksum formatted address string.
func ChecksumAddress(addr common.Address) string {
	// go-ethereum's Hex() already returns checksum format
	return addr.Hex()
}

// AddressFromHex parses and validates an Ethereum address.
func AddressFromHex(s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("invalid ethereum address: %s", s)
	}
	return common.HexToAddress(s), nil
}

// PrivateKeyFromHex loads a private key from a hex string (for testing).
func PrivateKeyFromHex(hexKey string) (*ecdsa.PrivateKey, error) {
	return crypto.HexToECDSA(strings.TrimPrefix(hexKey, "0x"))
}

// GenerateKey generates a new Ethereum private key (for testing).
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return crypto.GenerateKey()
}

// PrivateKeyToHex converts a private key to a hex string (for testing).
func PrivateKeyToHex(key *ecdsa.PrivateKey) string {
	return hex.EncodeToString(key.D.FillBytes(make([]byte, 32)))
}
