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

// BLEStats tracks per-interface capture counters. Fields match the linux version.
type BLEStats struct {
	Received atomic.Int64
	Parsed   atomic.Int64
	Errors   atomic.Int64
}

func RunBLE(_ context.Context, iface string, _ chan<- event.CaptureEvent, _ *oui.DB, _, _ string, _ *BLEStats) error {
	return errors.New("ble capture requires Linux")
}

func LogBLEStats(iface string, s *BLEStats) {
	log.Printf("stats: ble  %-8s  rx=%-8d parsed=%-8d errors=%d",
		iface, s.Received.Load(), s.Parsed.Load(), s.Errors.Load())
}
