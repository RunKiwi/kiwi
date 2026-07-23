package provider

import "context"

// Embedder defines an interface for obtaining text embeddings.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
