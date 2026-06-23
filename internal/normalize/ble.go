// Package normalize — BLE advertising report parser.
// Input comes from the HCI LE Advertising Report subevent (raw bytes from
// the capture package). No I/O, no goroutines.
package normalize

import (
	"fmt"
	"time"

	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

// ParseBLEReport converts a single parsed LE Advertising Report into a
// CaptureEvent. Returns nil if the report is malformed beyond recovery.
//
// addr    is the 6-byte little-endian address as received from HCI.
// addrType is 0x00 (public) or 0x01 (random).
// data    is the raw AD structure payload (the Data field of the report).
// rssi    is in dBm (int8 from HCI, already signed).
func ParseBLEReport(
	addr [6]byte,
	addrType uint8,
	data []byte,
	rssi int8,
	sessionID, nodeID, ifaceID string,
	db *oui.DB,
) *event.CaptureEvent {
	mac := bleMAC(addr)
	bleName, bleMfr := parseADStructures(data)

	return &event.CaptureEvent{
		Timestamp:  time.Now().UTC(),
		SessionID:  sessionID,
		NodeID:     nodeID,
		IfaceID:    ifaceID,
		Type:       "ble_adv",
		MAC:        mac,
		Vendor:     db.Lookup(mac),
		SSID:       "",
		ProbeSSIDs: []string{},
		RSSI:       int(rssi),
		Channel:    0,
		Encryption: "",
		BLEName:    bleName,
		BLEMfr:     bleMfr,
		Lat:        0.0,
		Lon:        0.0,
	}
}

// bleMAC converts the HCI little-endian 6-byte address to a colon-separated
// uppercase MAC string (MSB first, matching OUI lookup convention).
func bleMAC(addr [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		addr[5], addr[4], addr[3], addr[2], addr[1], addr[0])
}

// parseADStructures walks the raw AD structure bytes and returns
// (ble_name, ble_mfr). Both are empty string if the relevant AD types
// are absent. MAC is never used as a fallback for ble_name (AC-021).
func parseADStructures(data []byte) (bleName, bleMfr string) {
	for i := 0; i < len(data); {
		if i >= len(data) {
			break
		}
		adLen := int(data[i])
		if adLen == 0 {
			break // length=0 terminates the AD list
		}
		i++
		if i+adLen > len(data) {
			break // malformed — truncated
		}

		adType := data[i]
		adData := data[i+1 : i+adLen]
		i += adLen

		switch adType {
		case 0x08, 0x09: // Shortened / Complete Local Name
			// Prefer Complete (0x09) over Shortened (0x08) if both appear.
			if adType == 0x09 || bleName == "" {
				bleName = string(adData)
			}

		case 0xFF: // Manufacturer Specific Data
			// First 2 bytes = company ID, little-endian.
			if len(adData) >= 2 && bleMfr == "" {
				companyID := uint16(adData[0]) | uint16(adData[1])<<8
				bleMfr = fmt.Sprintf("0x%04X", companyID)
			}
		}
	}
	return bleName, bleMfr
}

