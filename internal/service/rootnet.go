package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RootNetClient interacts with the AWP RootNet API.
type RootNetClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewRootNetClient(baseURL string) *RootNetClient {
	return &RootNetClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// addressCheckResponse is the response from GET /api/address/{address}/check
type addressCheckResponse struct {
	// New format
	IsRegistered bool   `json:"isRegistered"`
	BoundTo      string `json:"boundTo"`
	Recipient    string `json:"recipient"`
	// Legacy format (kept for backward compatibility)
	IsRegisteredUser  bool `json:"isRegisteredUser"`
	IsRegisteredAgent bool `json:"isRegisteredAgent"`
}

// IsRegistered checks if the given address is registered on the RootNet.
func (c *RootNetClient) IsRegistered(ctx context.Context, address string) (bool, error) {
	url := fmt.Sprintf("%s/api/address/%s/check", c.BaseURL, address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request rootnet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("rootnet returned status %d", resp.StatusCode)
	}

	var result addressCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	return result.IsRegistered || result.IsRegisteredUser || result.IsRegisteredAgent, nil
}

// userResponse is the response from GET /api/users/{address}
type userResponse struct {
	User            json.RawMessage `json:"user"`
	RewardRecipient struct {
		UserAddress      string `json:"user_address"`
		RecipientAddress string `json:"recipient_address"`
	} `json:"rewardRecipient"`
}

// GetRewardRecipient returns the reward recipient address for the given miner.
// If no custom recipient is set, returns the miner's own address.
func (c *RootNetClient) GetRewardRecipient(ctx context.Context, workerAddress string) (string, error) {
	url := fmt.Sprintf("%s/api/users/%s", c.BaseURL, workerAddress)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request rootnet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rootnet returned status %d", resp.StatusCode)
	}

	var result userResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	recipient := result.RewardRecipient.RecipientAddress
	if recipient == "" || recipient == "0x0000000000000000000000000000000000000000" {
		return strings.ToLower(workerAddress), nil
	}
	return strings.ToLower(recipient), nil
}
