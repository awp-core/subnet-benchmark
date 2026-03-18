package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	privKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	expectedAddr := AddressFromPrivateKey(privKey)

	message := "POST/api/v1/questions1710000000abc123"
	sig, err := SignMessage(privKey, message)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHex := "0x" + hex.EncodeToString(sig)

	tests := []struct {
		name    string
		msg     string
		sig     string
		wantErr bool
		wantAddr string
	}{
		{
			name:     "valid signature",
			msg:      message,
			sig:      sigHex,
			wantAddr: strings.ToLower(expectedAddr.Hex()),
		},
		{
			name:    "wrong message",
			msg:     "tampered message",
			sig:     sigHex,
			wantAddr: "", // will recover a different address
		},
		{
			name:    "invalid hex",
			msg:     message,
			sig:     "0xinvalid",
			wantErr: true,
		},
		{
			name:    "wrong length",
			msg:     message,
			sig:     "0x" + hex.EncodeToString([]byte("short")),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := VerifySignature(tt.msg, tt.sig)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotAddr := strings.ToLower(addr.Hex())
			if tt.wantAddr != "" && gotAddr != tt.wantAddr {
				t.Errorf("addr = %s, want %s", gotAddr, tt.wantAddr)
			}
			if tt.wantAddr == "" && gotAddr == strings.ToLower(expectedAddr.Hex()) {
				t.Error("tampered message should recover different address")
			}
		})
	}
}

func TestBuildSignMessage(t *testing.T) {
	got := BuildSignMessage("POST", "/api/v1/questions", "1710000000", "abc123")
	want := "POST/api/v1/questions1710000000abc123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHashBody(t *testing.T) {
	h := HashBody([]byte(`{"test": true}`))
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64", len(h))
	}
}
