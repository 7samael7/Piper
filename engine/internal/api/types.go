package api

import (
	"encoding/json"

	"github.com/pipeline-workbench/engine/internal/pipeline/model"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type discoverRequest struct {
	RepoPath string           `json:"repoPath"`
	Provider model.ProviderID `json:"provider,omitempty"`
}

type workflowRequest struct {
	RepoPath     string           `json:"repoPath"`
	WorkflowPath string           `json:"workflowPath"`
	Provider     model.ProviderID `json:"provider,omitempty"`
}

type cancelRequest struct {
	RunID string `json:"runId"`
}

type cancelResponse struct {
	Cancelled bool `json:"cancelled"`
}

type historyRequest struct {
	RepoPath string `json:"repoPath,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type providerInfo struct {
	ID           model.ProviderID `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Capabilities []capability     `json:"capabilities"`
}

type capability struct {
	Name    string             `json:"name"`
	Support model.SupportLevel `json:"support"`
}
