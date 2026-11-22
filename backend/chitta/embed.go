package chitta

import (
	"context"
	"os"
	"path/filepath"
	"time"

	fastembed "github.com/anush008/fastembed-go"

	"zbot/backend/internal/domain"
)

// BGEQueryPrefix is the instruction fastembed's bge models expect on
// query-side inputs (passage inputs take none).
const BGEQueryPrefix = "Represent this sentence for searching relevant passages: "

// Embedder is the trusted-host embedding producer (seam-visible only as
// an interface; the fastembed implementation lives here).
type Embedder interface {
	// EmbedQuery embeds one query string (with the BGE instruction
	// prefix).
	EmbedQuery(ctx context.Context, query string) ([]float64, error)
	// EmbedPassages embeds record contents.
	EmbedPassages(ctx context.Context, texts []string) ([][]float64, error)
	// Close releases the model.
	Close() error
}

// fastEmbedder wraps fastembed-go (ONNX Runtime, fully local once the
// model files are cached — the setup task warms the cache; the daemon
// never downloads at runtime when the cache is warm).
type fastEmbedder struct {
	model *fastembed.FlagEmbedding
}

// EmbedModelCacheWarm reports whether the setup-warmed model cache is
// present. The daemon calls this before constructing the producer: a
// cold cache would DOWNLOAD at runtime (local-first violation), so the
// producer stays off until `task setup:chitta` warms it.
func EmbedModelCacheWarm(cacheDir string) bool {
	entries, err := os.ReadDir(cacheDir)
	return err == nil && len(entries) > 0
}

// NewFastEmbedder builds the producer. cacheDir holds the model files
// (setup-warmed — call EmbedModelCacheWarm first); model must match the
// configured chitta embedding model.
func NewFastEmbedder(model string, cacheDir string) (Embedder, error) {
	// Map the config model id to the fastembed artifact.
	var art string
	switch model {
	case "", "BAAI/bge-small-en-v1.5":
		art = "fast-bge-small-en-v1.5"
	case "BAAI/bge-base-en-v1.5":
		art = "fast-bge-base-en-v1.5"
	default:
		return nil, domain.NewChittaError(domain.ErrInvalidInput,
			"embedding model %q has no fastembed artifact mapping", model)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, domain.NewChittaError(domain.ErrUnavailable, "embedder cache dir: %v", err)
	}
	m, err := fastembed.NewFlagEmbedding(&fastembed.InitOptions{
		Model:                fastembed.EmbeddingModel(art),
		CacheDir:             cacheDir,
		ShowDownloadProgress: boolPtr(false),
	})
	if err != nil {
		return nil, domain.NewChittaError(domain.ErrUnavailable, "fastembed init: %v", err)
	}
	return &fastEmbedder{model: m}, nil
}

func (f *fastEmbedder) EmbedQuery(ctx context.Context, query string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vec, err := f.model.QueryEmbed(BGEQueryPrefix + query)
	if err != nil {
		return nil, domain.NewChittaError(domain.ErrUnavailable, "query embed: %v", err)
	}
	return f32to64(vec), nil
}

func (f *fastEmbedder) EmbedPassages(ctx context.Context, texts []string) ([][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vecs, err := f.model.Embed(texts, 16)
	if err != nil {
		return nil, domain.NewChittaError(domain.ErrUnavailable, "passage embed: %v", err)
	}
	out := make([][]float64, 0, len(vecs))
	for _, v := range vecs {
		out = append(out, f32to64(v))
	}
	return out, nil
}

func (f *fastEmbedder) Close() error { return f.model.Destroy() }

func f32to64(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}

func boolPtr(b bool) *bool { return &b }

// embedWorker is the background batch producer: periodically finds
// records lacking embeddings and writes them through the trusted seam.
// Failures degrade to keyword+graph recall — never a recall error.
type embedWorker struct {
	sys      *System
	embedder Embedder
	interval time.Duration
	stop     chan struct{}
	log      func(format string, args ...any)
}

// StartEmbedWorker launches the background producer (one per System).
// It is a no-op when the embedder is nil (producer not configured).
func (s *System) StartEmbedWorker(e Embedder, interval time.Duration, log func(string, ...any)) {
	if e == nil {
		return
	}
	w := &embedWorker{sys: s, embedder: e, interval: interval, stop: make(chan struct{}), log: log}
	if w.log == nil {
		w.log = func(string, ...any) {}
	}
	go w.loop()
	s.embedStop = w.stop
}

func (w *embedWorker) loop() {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.tick()
		}
	}
}

func (w *embedWorker) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // ctx-ok: F-19 embedding worker root
	defer cancel()
	ids, texts, err := w.sys.store.RecordsLackingEmbeddings(ctx, 32)
	if err != nil {
		w.log("chitta embedder: scan: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	vecs, err := w.embedder.EmbedPassages(ctx, texts)
	if err != nil {
		w.log("chitta embedder: embed degraded (keyword+graph recall only): %v", err)
		return
	}
	emb := make([]domain.RecordEmbedding, 0, len(ids))
	for i, id := range ids {
		if i < len(vecs) {
			emb = append(emb, domain.RecordEmbedding{RecordID: id, Embedding: vecs[i]})
		}
	}
	if err := w.sys.store.SetEmbeddings(ctx, emb); err != nil {
		w.log("chitta embedder: write degraded: %v", err)
	}
}

// DefaultEmbedModelDir is the conventional setup-warmed model cache.
func DefaultEmbedModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", "fastembed")
	}
	return filepath.Join(home, ".cache", "fastembed")
}
