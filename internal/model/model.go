// Package model defines bounded code proposals, independent of a provider SDK.
package model

import "context"

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type Request struct {
	Stage    string `json:"stage"`
	Issue    string `json:"issue"`
	Files    []File `json:"files"`
	Feedback string `json:"feedback,omitempty"`
}
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
type Proposal struct {
	Summary    string `json:"summary"`
	Files      []File `json:"files"`
	ResponseID string `json:"response_id,omitempty"`
	Model      string `json:"model,omitempty"`
	Usage      Usage  `json:"usage"`
}
type Provider interface {
	Propose(context.Context, Request) (Proposal, error)
}
