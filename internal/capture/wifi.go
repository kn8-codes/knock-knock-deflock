//go:build linux

// Raw AF_PACKET socket capture for 802.11 monitor-mode interfaces.
// gopacket/afpacket is deliberately avoided: its pageSize symbol is CGO-backed
// and breaks cross-compilation with CGO_ENABLED=0.  We open the socket
// ourselves using golang.org/x/sys/unix, then hand the raw bytes to gopacket
// only for decoding (the layers package is pure Go).
package capture

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync/atomic"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/sys/unix"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/normalize"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

const maxFrameSize = 4096 // well above the 802.11 management frame max (2346 B)

// WiFiStats tracks per-interface capture counters. All fields are atomic.
type WiFiStats struct {
	Received atomic.Int64
	Parsed   atomic.Int64
	Errors   atomic.Int64
}

// RunWiFi reads 802.11 radiotap frames from iface (which must already be in
// monitor mode) and sends normalised CaptureEvents to out. Returns when ctx
// is cancelled or the socket encounters an unrecoverable error.
func RunWiFi(
	ctx context.Context,
	iface string,
	out chan<- event.CaptureEvent,
	db *oui.DB,
	sessionID, nodeID string,
	stats *WiFiStats,
) error {
	fd, err := openRawSock(iface)
	if err != nil {
		return fmt.Errorf("wifi %s: %w", iface, err)
	}
	defer unix.Close(fd)

	// 200 ms read timeout — lets us poll ctx without busy-spinning.
	tv := unix.Timeval{Sec: 0, Usec: 200_000}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("wifi %s: SO_RCVTIMEO: %w", iface, err)
	}

	buf := make([]byte, maxFrameSize)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isTransient(err) {
				continue // EAGAIN (timeout) or EINTR — normal
			}
			stats.Errors.Add(1)
			log.Printf("capture/wifi: %s: recvfrom: %v", iface, err)
			continue
		}
		if n == 0 {
			continue
		}
		stats.Received.Add(1)

		// Copy frame bytes — buf is reused on the next Recvfrom call.
		frame := make([]byte, n)
		copy(frame, buf[:n])

		pkt := gopacket.NewPacket(frame, layers.LinkTypeIEEE80211Radio, gopacket.Default)
		ev := normalize.ParseWiFiPacket(pkt, sessionID, nodeID, iface, db)
		if ev == nil {
			continue
		}
		stats.Parsed.Add(1)

		select {
		case out <- *ev:
		case <-ctx.Done():
			return nil
		}
	}
}

// LogWiFiStats emits a single stats line to stderr.
func LogWiFiStats(iface string, s *WiFiStats) {
	log.Printf("stats: wifi %-8s  rx=%-8d parsed=%-8d errors=%d",
		iface, s.Received.Load(), s.Parsed.Load(), s.Errors.Load())
}

// openRawSock opens an AF_PACKET / SOCK_RAW socket bound to iface.
// The socket captures all Ethernet-level frames (ETH_P_ALL).
func openRawSock(iface string) (int, error) {
	proto := htons(unix.ETH_P_ALL)
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(proto))
	if err != nil {
		return -1, fmt.Errorf("socket AF_PACKET: %w", err)
	}

	idx, err := ifaceIndex(iface)
	if err != nil {
		_ = unix.Close(fd)
		return -1, err
	}

	addr := &unix.SockaddrLinklayer{
		Protocol: proto,
		Ifindex:  idx,
	}
	if err := unix.Bind(fd, addr); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("bind %s: %w", iface, err)
	}
	return fd, nil
}

func ifaceIndex(name string) (int, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("interface %s: %w", name, err)
	}
	return ifc.Index, nil
}

// htons converts a uint16 from host to network (big-endian) byte order.
func htons(v uint16) uint16 { return v<<8 | v>>8 }

func isTransient(err error) bool {
	return errors.Is(err, unix.EAGAIN) ||
		errors.Is(err, unix.EINTR) ||
		errors.Is(err, unix.ETIMEDOUT)
}
