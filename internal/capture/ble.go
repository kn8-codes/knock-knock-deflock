//go:build linux

// HCI raw socket capture for BLE passive scanning.
// No BlueZ dependency — all operations use golang.org/x/sys/unix directly.
// Passive scan only (scan type 0x00). No LE Set Scan Enable with active flag.
package capture

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/normalize"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

// HCI constants not exported by golang.org/x/sys/unix.
const (
	hciEventPkt    = 0x04 // HCI packet type: event
	hciCmdComplete = 0x0E // HCI event code: command complete
	hciLeMetaEvent = 0x3E // HCI event code: LE meta
	hciLeAdvReport = 0x02 // LE meta subevent: advertising report
	solHCI         = 0    // SOL_HCI socket option level
	hciFilterOpt   = 2    // HCI_FILTER socket option
)

// hciFilter mirrors struct hci_filter in the kernel (14 bytes, no padding).
type hciFilter struct {
	TypeMask  uint32
	EventMask [2]uint32
	OpCode    uint16
}

// BLEStats tracks per-interface capture counters. All fields are atomic.
type BLEStats struct {
	Received atomic.Int64
	Parsed   atomic.Int64
	Errors   atomic.Int64
}

// RunBLE opens an HCI raw socket for iface (e.g. "hci0"), configures passive
// BLE scanning, and emits CaptureEvents to out. Returns when ctx is cancelled
// or the socket encounters an unrecoverable error.
func RunBLE(
	ctx context.Context,
	iface string,
	out chan<- event.CaptureEvent,
	db *oui.DB,
	sessionID, nodeID string,
	stats *BLEStats,
) error {
	devIdx, err := hciDevIdx(iface)
	if err != nil {
		return fmt.Errorf("ble %s: %w", iface, err)
	}

	fd, err := openHCISock(devIdx)
	if err != nil {
		return fmt.Errorf("ble %s: %w", iface, err)
	}
	defer unix.Close(fd)

	if err := setHCIFilter(fd); err != nil {
		return fmt.Errorf("ble %s: filter: %w", iface, err)
	}

	// Set a longer timeout during scan setup so command complete reads don't block forever.
	tv := unix.Timeval{Sec: 5, Usec: 0}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("ble %s: SO_RCVTIMEO setup: %w", iface, err)
	}

	if err := sendPassiveScan(fd); err != nil {
		return fmt.Errorf("ble %s: passive scan: %w", iface, err)
	}

	// Narrow timeout to 200ms for the main read loop.
	tv = unix.Timeval{Sec: 0, Usec: 200_000}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("ble %s: SO_RCVTIMEO loop: %w", iface, err)
	}

	buf := make([]byte, 260) // max HCI event = 1(type) + 1(code) + 1(plen) + 255(params)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isTransient(err) {
				continue
			}
			stats.Errors.Add(1)
			log.Printf("capture/ble: %s: read: %v", iface, err)
			continue
		}
		if n < 4 {
			continue
		}
		// HCI event packet: [0]=0x04 [1]=event_code [2]=param_len [3..]=params
		if buf[0] != hciEventPkt || buf[1] != hciLeMetaEvent {
			continue
		}
		if buf[3] != hciLeAdvReport {
			continue // LE meta event but not an advertising report
		}
		stats.Received.Add(1)

		for _, rep := range parseAdvReports(buf[4:n]) {
			ev := normalize.ParseBLEReport(rep.addr, rep.addrType, rep.data, rep.rssi, sessionID, nodeID, iface, db)
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
}

// LogBLEStats emits a single stats line to stderr.
func LogBLEStats(iface string, s *BLEStats) {
	log.Printf("stats: ble  %-8s  rx=%-8d parsed=%-8d errors=%d",
		iface, s.Received.Load(), s.Parsed.Load(), s.Errors.Load())
}

// openHCISock opens an AF_BLUETOOTH/BTPROTO_HCI raw socket bound to devIdx.
func openHCISock(devIdx uint16) (int, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW, unix.BTPROTO_HCI)
	if err != nil {
		return -1, fmt.Errorf("socket AF_BLUETOOTH: %w", err)
	}
	sa := &unix.SockaddrHCI{Dev: devIdx, Channel: 0} // Channel=0 → HCI_CHANNEL_RAW
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("bind hci%d: %w", devIdx, err)
	}
	return fd, nil
}

// setHCIFilter installs an HCI socket filter that passes only HCI event packets.
func setHCIFilter(fd int) error {
	f := hciFilter{
		TypeMask:  1 << 4, // HCI_EVENT_PKT = packet type 4, bit 4
		EventMask: [2]uint32{0xFFFFFFFF, 0xFFFFFFFF},
		OpCode:    0, // any
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_SETSOCKOPT,
		uintptr(fd),
		solHCI,
		hciFilterOpt,
		uintptr(unsafe.Pointer(&f)),
		unsafe.Sizeof(f),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("setsockopt HCI_FILTER: %w", errno)
	}
	return nil
}

// sendPassiveScan sends LE Set Scan Parameters (passive) and LE Set Scan Enable.
// Each command waits for the corresponding Command Complete event before proceeding.
func sendPassiveScan(fd int) error {
	// LE Set Scan Parameters: type=passive(0x00), interval=16, window=16, own=public, policy=accept all
	params := []byte{0x01, 0x0B, 0x20, 0x07, 0x00, 0x10, 0x00, 0x10, 0x00, 0x00, 0x00}
	if err := sendHCICmd(fd, params); err != nil {
		return fmt.Errorf("LE Set Scan Parameters: %w", err)
	}

	// LE Set Scan Enable: enable=1, filter_duplicates=0
	enable := []byte{0x01, 0x0C, 0x20, 0x02, 0x01, 0x00}
	if err := sendHCICmd(fd, enable); err != nil {
		return fmt.Errorf("LE Set Scan Enable: %w", err)
	}
	return nil
}

// sendHCICmd writes an HCI command packet and reads until it receives the
// corresponding Command Complete event. Returns an error if the command status
// is non-zero or if no complete event arrives within maxAttempts reads.
func sendHCICmd(fd int, cmd []byte) error {
	if _, err := unix.Write(fd, cmd); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	// cmd[1..2] = opcode (little-endian)
	opcode := binary.LittleEndian.Uint16(cmd[1:3])

	buf := make([]byte, 260)
	const maxAttempts = 20
	for i := 0; i < maxAttempts; i++ {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if isTransient(err) {
				continue
			}
			return fmt.Errorf("read: %w", err)
		}
		// Command Complete event: [0]=type [1]=0x0E [2]=plen [3]=num_cmds [4..5]=opcode [6]=status
		if n < 7 || buf[0] != hciEventPkt || buf[1] != hciCmdComplete {
			continue
		}
		gotOpcode := binary.LittleEndian.Uint16(buf[4:6])
		if gotOpcode != opcode {
			continue
		}
		if buf[6] != 0x00 {
			return fmt.Errorf("status 0x%02X", buf[6])
		}
		return nil
	}
	return fmt.Errorf("no command complete for opcode 0x%04X", opcode)
}

// bleAdvReport holds the parsed fields from a single LE Advertising Report.
type bleAdvReport struct {
	addr     [6]byte
	addrType uint8
	data     []byte
	rssi     int8
}

// parseAdvReports parses the payload of an LE Advertising Report subevent
// starting after the subevent byte (i.e. data[0] = Num_Reports).
func parseAdvReports(data []byte) []bleAdvReport {
	if len(data) < 1 {
		return nil
	}
	numReports := int(data[0])
	offset := 1
	out := make([]bleAdvReport, 0, numReports)

	for i := 0; i < numReports; i++ {
		// Minimum per-report bytes: event_type(1) + addr_type(1) + addr(6) + data_len(1) = 9
		if offset+9 > len(data) {
			break
		}
		addrType := data[offset+1]
		var addr [6]byte
		copy(addr[:], data[offset+2:offset+8])
		dataLen := int(data[offset+8])
		offset += 9
		// data_len bytes + 1 byte RSSI
		if offset+dataLen+1 > len(data) {
			break
		}
		adData := make([]byte, dataLen)
		copy(adData, data[offset:offset+dataLen])
		rssi := int8(data[offset+dataLen])
		offset += dataLen + 1

		out = append(out, bleAdvReport{
			addr:     addr,
			addrType: addrType,
			data:     adData,
			rssi:     rssi,
		})
	}
	return out
}

// hciDevIdx parses the numeric index from an HCI device name ("hci0" → 0).
func hciDevIdx(iface string) (uint16, error) {
	var idx uint16
	if _, err := fmt.Sscanf(iface, "hci%d", &idx); err != nil {
		return 0, fmt.Errorf("cannot parse HCI device index from %q (expected hciN)", iface)
	}
	return idx, nil
}
