// Package emit writes CaptureEvents as newline-delimited JSON (NDJSON) to a
// local file with append semantics, fsync-per-write durability, and
// size/age-triggered rotation.
package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
)

// Writer is safe for concurrent use.
type Writer struct {
	path      string
	rotateSz  int64
	rotateAge time.Duration
	keep      int

	mu           sync.Mutex
	f            *os.File
	openedAt     time.Time
	bytesWritten int64
}

// New opens (or creates) the output file in append mode and returns a Writer.
func New(path string, rotateSz int64, rotateAge time.Duration, keep int) (*Writer, error) {
	w := &Writer{
		path:      path,
		rotateSz:  rotateSz,
		rotateAge: rotateAge,
		keep:      keep,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write serializes ev as a single JSON line and syncs to disk.
func (w *Writer) Write(ev event.CaptureEvent) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("emit: marshal: %w", err)
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.maybeRotate(); err != nil {
		return err
	}
	n, err := w.f.Write(line)
	if err != nil {
		return fmt.Errorf("emit: write: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("emit: sync: %w", err)
	}
	w.bytesWritten += int64(n)
	return nil
}

// Close flushes and closes the active file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// BytesWritten returns the number of bytes written to the current active file.
func (w *Writer) BytesWritten() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytesWritten
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("emit: open %s: %w", w.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("emit: stat %s: %w", w.path, err)
	}
	w.f = f
	w.openedAt = time.Now()
	w.bytesWritten = info.Size()
	return nil
}

// maybeRotate checks thresholds and rotates if needed. Caller holds w.mu.
func (w *Writer) maybeRotate() error {
	sizeTriggered := w.rotateSz > 0 && w.bytesWritten >= w.rotateSz
	ageTriggered := w.rotateAge > 0 && time.Since(w.openedAt) >= w.rotateAge
	if !sizeTriggered && !ageTriggered {
		return nil
	}
	return w.rotate()
}

func (w *Writer) rotate() error {
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("emit: rotate sync: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("emit: rotate close: %w", err)
	}
	w.f = nil

	rotated := rotatedName(w.path, time.Now().UTC())
	if err := os.Rename(w.path, rotated); err != nil {
		return fmt.Errorf("emit: rotate rename: %w", err)
	}

	if err := w.pruneOld(); err != nil {
		// non-fatal: log-worthy but don't abort capture
		fmt.Fprintf(os.Stderr, "emit: prune warning: %v\n", err)
	}

	if err := w.open(); err != nil {
		return fmt.Errorf("emit: rotate reopen: %w", err)
	}
	return nil
}

// rotatedName inserts a UTC timestamp before the file extension.
// "kkd-capture.ndjson" → "kkd-capture.20260623T143000Z.ndjson"
// "kkd-capture"        → "kkd-capture.20260623T143000Z"
func rotatedName(path string, t time.Time) string {
	ts := t.Format("20060102T150405Z")
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + "." + ts + ext
}

// pruneOld deletes the oldest completed rotation files if count > w.keep.
func (w *Writer) pruneOld() error {
	if w.keep <= 0 {
		return nil
	}
	dir := filepath.Dir(w.path)
	base := strings.TrimSuffix(filepath.Base(w.path), filepath.Ext(w.path))
	ext := filepath.Ext(w.path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var rotated []string
	for _, e := range entries {
		name := e.Name()
		// match pattern: base.<timestamp><ext>
		if !strings.HasPrefix(name, base+".") {
			continue
		}
		if ext != "" && !strings.HasSuffix(name, ext) {
			continue
		}
		// exclude the active file itself
		if name == filepath.Base(w.path) {
			continue
		}
		rotated = append(rotated, filepath.Join(dir, name))
	}

	sort.Strings(rotated) // lexicographic = chronological for our timestamp format

	for len(rotated) > w.keep {
		if err := os.Remove(rotated[0]); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", rotated[0], err)
		}
		rotated = rotated[1:]
	}
	return nil
}
