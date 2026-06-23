//go:build !linux

package capture

import (
	"context"
	"errors"
	"log"
	"sync/atomic"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

// WiFiStats mirrors the Linux definition so main.go compiles on all platforms.
type WiFiStats struct {
	Received atomic.Int64
	Parsed   atomic.Int64
	Errors   atomic.Int64
}

func RunWiFi(_ context.Context, _ string, _ chan<- event.CaptureEvent, _ *oui.DB, _, _ string, _ *WiFiStats) error {
	return errors.New("wifi capture requires Linux")
}

func LogWiFiStats(iface string, s *WiFiStats) {
	log.Printf("stats: wifi %-8s  rx=%-8d parsed=%-8d errors=%d",
		iface, s.Received.Load(), s.Parsed.Load(), s.Errors.Load())
}
