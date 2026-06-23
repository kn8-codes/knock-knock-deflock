package emit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
)

func makeEvent(t *testing.T) event.CaptureEvent {
	t.Helper()
	return event.CaptureEvent{
		Timestamp:  time.Now().UTC(),
		SessionID:  "test-session",
		Type:       "wifi_beacon",
		MAC:        "AA:BB:CC:DD:EE:FF",
		Vendor:     "Test Vendor",
		SSID:       "TestNet",
		ProbeSSIDs: []string{},
		RSSI:       -60,
		Channel:    6,
		Encryption: "wpa2",
		BLEName:    "",
		BLEMfr:     "",
		Lat:        0.0,
		Lon:        0.0,
		NodeID:     "node-001",
		IfaceID:    "wlan0",
	}
}

// AC-060: every line is valid JSON parseable into CaptureEvent
func TestWriteValidNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ndjson")
	w, err := New(path, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 5; i++ {
		if err := w.Write(makeEvent(t)); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	w.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var ev event.CaptureEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Errorf("line %d invalid JSON: %v — %s", n+1, err, sc.Text())
		}
		n++
	}
	if n != 5 {
		t.Errorf("expected 5 lines, got %d", n)
	}
}

// AC-061: reopen appends, no truncation
func TestAppendOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ndjson")

	w1, err := New(path, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w1.Write(makeEvent(t))
	}
	w1.Close()

	w2, err := New(path, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w2.Write(makeEvent(t))
	}
	w2.Close()

	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
	}
	if n != 5 {
		t.Errorf("expected 5 lines after reopen, got %d (want append, not truncate)", n)
	}
}

// AC-063: rotation triggers at size threshold
func TestRotateOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cap.ndjson")

	// set rotate threshold to 1 byte so every write triggers rotation
	w, err := New(path, 1, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		if err := w.Write(makeEvent(t)); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	w.Close()

	entries, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range entries {
		if e.Name() != "cap.ndjson" {
			rotated++
		}
	}
	if rotated == 0 {
		t.Error("expected at least one rotated file, found none")
	}
}

// AC-064: rotated filename has expected timestamp pattern
func TestRotatedName(t *testing.T) {
	ts := time.Date(2026, 6, 23, 14, 30, 0, 0, time.UTC)
	cases := []struct{ path, want string }{
		{"./kkd-capture.ndjson", "./kkd-capture.20260623T143000Z.ndjson"},
		{"./kkd-capture", "./kkd-capture.20260623T143000Z"},
		{"/mnt/data/cap.ndjson", "/mnt/data/cap.20260623T143000Z.ndjson"},
	}
	for _, tc := range cases {
		got := rotatedName(tc.path, ts)
		if got != tc.want {
			t.Errorf("rotatedName(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// AC-065: keep limit is enforced
func TestRotateKeep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cap.ndjson")

	const keep = 3
	w, err := New(path, 1, 0, keep)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// write enough events to produce keep+2 rotations
	for i := 0; i < keep+2+1; i++ {
		w.Write(makeEvent(t))
	}
	w.Close()

	entries, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range entries {
		if e.Name() != "cap.ndjson" {
			rotated++
		}
	}
	if rotated > keep {
		t.Errorf("expected at most %d rotated files, found %d", keep, rotated)
	}
}
