// Package tfidf implements a lightweight text vectorizer using the hashing trick.
//
// Algorithm:
//  1. Tokenize — lowercase, split on non-letter/non-digit runes, drop single-char tokens.
//  2. Count term frequencies per bucket (FNV-32a hash mod dim).
//  3. Apply sublinear TF scaling: weight = 1 + log(tf).
//  4. L2-normalize the resulting vector so cosine similarity works correctly.
//
// No vocabulary or corpus is needed. The output dimension is fixed at construction time
// and must match the `embedding_dim` value in the Codohue namespace config.
package tfidf

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Vectorizer converts text into a dense float64 vector of fixed dimension.
// It is safe for concurrent use — Vectorize holds no mutable state.
type Vectorizer struct {
	dim int
}

// New creates a Vectorizer that produces vectors of length dim.
// dim must be >= 1. Codohue's default embedding_dim is 64.
func New(dim int) *Vectorizer {
	if dim < 1 {
		dim = 64
	}
	return &Vectorizer{dim: dim}
}

// Vectorize converts text into a dense L2-normalized float64 vector.
// The hashing trick maps tokens into dim buckets — collisions are rare at dim >= 64
// and do not produce incorrect results, only mild signal mixing.
func (v *Vectorizer) Vectorize(text string) []float64 {
	counts := make(map[int]float64)
	for _, tok := range tokenize(text) {
		h := bucket(tok, v.dim)
		counts[h]++
	}

	vec := make([]float64, v.dim)
	var norm float64
	for i, c := range counts {
		// Sublinear TF scaling reduces the dominance of highly repeated terms.
		w := 1 + math.Log(c)
		vec[i] = w
		norm += w * w
	}

	// L2 normalize so all vectors lie on the unit sphere — required for cosine distance.
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec
}

// Embed implements the EmbeddingProvider interface expected by post.Service.
// context is accepted for interface compatibility but not used (no I/O).
func (v *Vectorizer) Embed(_ context.Context, text string) ([]float64, error) {
	return v.Vectorize(text), nil
}

// Dim returns the output vector dimension.
func (v *Vectorizer) Dim() int { return v.dim }

// tokenize lowercases text and splits on any rune that is not a Unicode letter
// or digit. Tokens shorter than 2 runes are dropped to reduce noise.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	result := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len([]rune(t)) >= 2 {
			result = append(result, t)
		}
	}
	return result
}

// bucket maps a token to a bucket index in [0, dim) using FNV-32a.
func bucket(token string, dim int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	idx := int(h.Sum32()) % dim
	if idx < 0 {
		idx += dim
	}
	return idx
}
