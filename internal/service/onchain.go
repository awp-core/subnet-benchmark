package service

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/awp-core/subnet-benchmark/internal/store"
)

// OnchainConfig holds configuration for on-chain interactions.
type OnchainConfig struct {
	RPCURL          string // Chain RPC endpoint
	ContractAddress string // SubnetManager contract address
	PrivateKeyHex   string // Backend wallet private key with MERKLE_ROLE (hex, no 0x prefix)
	ChainID         int64  // Chain ID
}

// OnchainService handles submitting merkle roots to the SubnetManager contract.
type OnchainService struct {
	Store  *store.Store
	Config OnchainConfig
}

// PublishMerkleRoot submits the merkle root for an epoch to the SubnetManager contract.
func (svc *OnchainService) PublishMerkleRoot(ctx context.Context, epochDate time.Time) error {
	// 1. Get merkle root from database
	root, err := svc.Store.GetEpochMerkleRoot(ctx, epochDate)
	if err != nil {
		return fmt.Errorf("get merkle root: %w", err)
	}
	if root == "" {
		return fmt.Errorf("no merkle root for epoch %s", epochDate.Format("2006-01-02"))
	}

	// 2. Connect to chain
	client, err := ethclient.DialContext(ctx, svc.Config.RPCURL)
	if err != nil {
		return fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()

	// 3. Load private key
	privateKey, err := crypto.HexToECDSA(svc.Config.PrivateKeyHex)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// 4. Encode function call: setMerkleRoot(uint32 epoch, bytes32 merkleRoot)
	epoch := dateToEpochUint32(epochDate)
	rootBytes := common.HexToHash(root)

	data, err := packSetMerkleRoot(epoch, rootBytes)
	if err != nil {
		return fmt.Errorf("pack call data: %w", err)
	}

	// 5. Build and sign transaction
	contractAddr := common.HexToAddress(svc.Config.ContractAddress)
	chainID := big.NewInt(svc.Config.ChainID)

	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return fmt.Errorf("get nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("suggest gas price: %w", err)
	}

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), 200000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	// 6. Send transaction
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}

	// 7. Wait for receipt
	receipt, err := waitForReceipt(ctx, client, signedTx.Hash())
	if err != nil {
		return fmt.Errorf("wait for receipt: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("tx reverted: %s", signedTx.Hash().Hex())
	}

	// 8. Mark epoch as published
	if err := svc.Store.SetEpochPublished(ctx, epochDate); err != nil {
		return fmt.Errorf("set epoch published: %w", err)
	}

	return nil
}

// dateToEpochUint32 converts a date to YYYYMMDD uint32.
func dateToEpochUint32(t time.Time) uint32 {
	y, m, d := t.Date()
	return uint32(y*10000 + int(m)*100 + d)
}

// packSetMerkleRoot encodes the setMerkleRoot(uint32,bytes32) call data.
func packSetMerkleRoot(epoch uint32, root common.Hash) ([]byte, error) {
	uint32Ty, _ := abi.NewType("uint32", "", nil)
	bytes32Ty, _ := abi.NewType("bytes32", "", nil)

	method := abi.NewMethod("setMerkleRoot", "setMerkleRoot", abi.Function, "", false, false,
		abi.Arguments{
			{Name: "epoch", Type: uint32Ty},
			{Name: "merkleRoot", Type: bytes32Ty},
		},
		nil,
	)

	packed, err := method.Inputs.Pack(epoch, root)
	if err != nil {
		return nil, err
	}

	return append(method.ID, packed...), nil
}

// waitForReceipt polls for a transaction receipt.
func waitForReceipt(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// dummy reference to suppress unused import
var _ *ecdsa.PrivateKey
