// Package tts turns Korean text into a wav file, caching every result on disk.
//
// Synthesis is the slowest thing in the app and its output never changes for
// the same input, so the cache is not an optimization — it is the reason audio
// feels instant after the first pass. A pre-warm run over a chapter's vocab
// makes every card in that chapter silent-fast forever.
package tts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Errors callers distinguish: both mean the request was bad, not the sidecar.
var (
	ErrEmptyText   = errors.New("text is empty")
	ErrTextTooLong = errors.New("text is too long")
)

// MaxTextLen caps a single synthesis request. Vocabulary and example sentences
// are short; anything longer is a bug in the caller, and the sidecar holds a
// single model that a long request would block for everyone.
const MaxTextLen = 500

type Cache struct {
	// Dir holds the wav files. Created on first use.
	Dir string
	// SidecarURL is the base URL of the Python synthesis service.
	SidecarURL string
	// Voice selects the sidecar voice. It is part of the cache key, so
	// changing it does not serve audio in the previous voice.
	Voice  string
	Client *http.Client
}

func New(dir, sidecarURL, voice string) *Cache {
	return &Cache{
		Dir:        dir,
		SidecarURL: sidecarURL,
		Voice:      voice,
		// Model load on the sidecar's first request is slow; steady-state
		// synthesis is not.
		Client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Path returns the wav file for text, synthesizing it if it is not cached yet.
func (c *Cache) Path(ctx context.Context, text string) (string, error) {
	if text == "" {
		return "", ErrEmptyText
	}
	if len(text) > MaxTextLen {
		return "", fmt.Errorf("%w: %d bytes, limit is %d", ErrTextTooLong, len(text), MaxTextLen)
	}

	path := filepath.Join(c.Dir, c.key(text)+".wav")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	audio, err := c.synthesize(ctx, text)
	if err != nil {
		return "", err
	}
	if err := c.write(path, audio); err != nil {
		return "", err
	}
	return path, nil
}

// key hashes voice and text together. Two texts never collide, and the same
// text in a different voice gets its own file.
func (c *Cache) key(text string) string {
	sum := sha256.Sum256([]byte(c.Voice + "\x00" + text))
	return hex.EncodeToString(sum[:16])
}

func (c *Cache) synthesize(ctx context.Context, text string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"text": text, "voice": c.Voice})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SidecarURL+"/synth", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts sidecar unreachable at %s: %w", c.SidecarURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("tts sidecar returned %s: %s", resp.Status, bytes.TrimSpace(detail))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read synthesized audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("tts sidecar returned no audio")
	}
	return audio, nil
}

// write saves audio atomically: a crash or a second request racing this one
// must never leave a truncated wav in the cache, because nothing would ever
// invalidate it and every later play would be silent.
func (c *Cache) write(path string, audio []byte) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tmp, err := os.CreateTemp(c.Dir, "synth-*.wav.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds

	if _, err := tmp.Write(audio); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
