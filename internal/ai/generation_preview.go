package ai

import "context"

type generationPreviewKey struct{}
type generationPreview func(string, string) error

// Previews are tentative, reauthorized and filtered. Only the completed message
// is durable; an interrupted stream never becomes a successful generation.
func withGenerationPreview(ctx context.Context, fn generationPreview) context.Context {
	return context.WithValue(ctx, generationPreviewKey{}, fn)
}
func generationPreviewFrom(ctx context.Context) generationPreview {
	v, _ := ctx.Value(generationPreviewKey{}).(generationPreview)
	return v
}
