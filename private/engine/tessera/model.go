package tessera

import "github.com/ethereum/go-ethereum/private/engine"

// request object for /send API
type sendRequest struct {
	Payload []byte `json:"payload"`

	// base64-encoded
	From string `json:"from,omitempty"`

	To []string `json:"to"`

	// Transactions' encrypted payload hashes for affected contracts
	AffectedContractTransactions []string `json:"affectedContractTransactions"`

	// Merkle root for affected contracts
	ExecHash string `json:"execHash,omitempty"`

	PrivacyFlag engine.PrivacyFlagType `json:"privacyFlag"`
}

// response object for /send API
type sendResponse struct {
	// Base64-encoded
	Key string `json:"key"`
}

type receiveResponse struct {
	Payload []byte `json:"payload"`

	// Transactions' encrypted payload hashes for affected contracts
	AffectedContractTransactions []string `json:"affectedContractTransactions"`

	// Merkle root for affected contracts
	ExecHash string `json:"execHash"`

	PrivacyFlag engine.PrivacyFlagType `json:"privacyFlag"`
}

type sendSignedTxRequest struct {
	Hash []byte   `json:"hash"`
	To   []string `json:"to"`
	// Transactions' encrypted payload hashes for affected contracts
	AffectedContractTransactions []string `json:"affectedContractTransactions"`
	// Merkle root for affected contracts
	ExecHash string `json:"execHash,omitempty"`

	PrivacyFlag engine.PrivacyFlagType `json:"privacyFlag"`
}

type sendSignedTxResponse struct {
	// Base64-encoded
	Key string `json:"key"`
}
