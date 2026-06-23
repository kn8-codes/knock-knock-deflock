// Package normalize parses raw 802.11 packets into CaptureEvents.
// No I/O, no goroutines — pure parsing, safe for concurrent use.
package normalize

import (
	"encoding/binary"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

// ParseWiFiPacket decodes a radiotap+802.11 packet into a CaptureEvent.
// Returns nil for frame types that are not in scope (silently skipped).
func ParseWiFiPacket(pkt gopacket.Packet, sessionID, nodeID, ifaceID string, db *oui.DB) *event.CaptureEvent {
	rtLayer := pkt.Layer(layers.LayerTypeRadioTap)
	if rtLayer == nil {
		return nil
	}
	rt := rtLayer.(*layers.RadioTap)

	dot11Layer := pkt.Layer(layers.LayerTypeDot11)
	if dot11Layer == nil {
		return nil
	}
	dot11 := dot11Layer.(*layers.Dot11)

	rssi := int(rt.DBMAntennaSignal)
	ch := freqToChannel(uint16(rt.ChannelFrequency))

	switch dot11.Type {
	case layers.Dot11TypeMgmtBeacon:
		return parseBeacon(pkt, dot11, rt, rssi, ch, sessionID, nodeID, ifaceID, db)
	case layers.Dot11TypeMgmtProbeReq:
		return parseProbeReq(pkt, dot11, rssi, ch, sessionID, nodeID, ifaceID, db)
	}
	return nil
}

func parseBeacon(
	pkt gopacket.Packet,
	dot11 *layers.Dot11,
	rt *layers.RadioTap,
	rssi, rtCh int,
	sessionID, nodeID, ifaceID string,
	db *oui.DB,
) *event.CaptureEvent {
	mac := dot11.Address2.String()

	ssid := ""
	channel := rtCh
	enc := "open"
	hasPrivacy := false

	if bl := pkt.Layer(layers.LayerTypeDot11MgmtBeacon); bl != nil {
		b := bl.(*layers.Dot11MgmtBeacon)
		hasPrivacy = b.Flags&0x0010 != 0
	}

	for _, ie := range collectIEs(pkt, layers.Dot11TypeMgmtBeacon) {
		switch ie.ID {
		case layers.Dot11InformationElementIDSSID:
			ssid = string(ie.Info)
		case layers.Dot11InformationElementIDDSSet:
			if len(ie.Info) > 0 {
				channel = int(ie.Info[0])
			}
		case layers.Dot11InformationElementIDRSNInfo:
			enc = parseRSN(ie.Info)
		case layers.Dot11InformationElementIDVendor:
			if isWPAVendorIE(ie.Info) && enc == "open" {
				enc = "wpa"
			}
		}
	}

	if enc == "open" && hasPrivacy {
		enc = "wep"
	}

	return &event.CaptureEvent{
		Timestamp:  time.Now().UTC(),
		SessionID:  sessionID,
		NodeID:     nodeID,
		IfaceID:    ifaceID,
		Type:       "wifi_beacon",
		MAC:        mac,
		Vendor:     db.Lookup(mac),
		SSID:       ssid,
		ProbeSSIDs: []string{},
		RSSI:       rssi,
		Channel:    channel,
		Encryption: enc,
		BLEName:    "",
		BLEMfr:     "",
		Lat:        0.0,
		Lon:        0.0,
	}
}

func parseProbeReq(
	pkt gopacket.Packet,
	dot11 *layers.Dot11,
	rssi, rtCh int,
	sessionID, nodeID, ifaceID string,
	db *oui.DB,
) *event.CaptureEvent {
	mac := dot11.Address2.String()
	channel := rtCh
	var probeSSIDs []string

	for _, ie := range collectIEs(pkt, layers.Dot11TypeMgmtProbeReq) {
		switch ie.ID {
		case layers.Dot11InformationElementIDSSID:
			if len(ie.Info) > 0 {
				probeSSIDs = append(probeSSIDs, string(ie.Info))
			}
		case layers.Dot11InformationElementIDDSSet:
			if len(ie.Info) > 0 {
				channel = int(ie.Info[0])
			}
		case layers.Dot11InformationElementIDSSIDList:
			// 802.11k Extended SSID List IE — parse individual SSIDs
			for i := 0; i < len(ie.Info); {
				if i+1 >= len(ie.Info) {
					break
				}
				slen := int(ie.Info[i+1])
				i += 2
				if i+slen > len(ie.Info) {
					break
				}
				if slen > 0 {
					probeSSIDs = append(probeSSIDs, string(ie.Info[i:i+slen]))
				}
				i += slen
			}
		}
	}

	if probeSSIDs == nil {
		probeSSIDs = []string{}
	}

	return &event.CaptureEvent{
		Timestamp:  time.Now().UTC(),
		SessionID:  sessionID,
		NodeID:     nodeID,
		IfaceID:    ifaceID,
		Type:       "wifi_probe",
		MAC:        mac,
		Vendor:     db.Lookup(mac),
		SSID:       "",
		ProbeSSIDs: probeSSIDs,
		RSSI:       rssi,
		Channel:    channel,
		Encryption: "",
		BLEName:    "",
		BLEMfr:     "",
		Lat:        0.0,
		Lon:        0.0,
	}
}

// collectIEs returns the IEs for a management frame. It first tries the
// gopacket layer chain (reliable for beacons in v1.1.19), then falls back to
// manual parsing from the Dot11 payload for frames where gopacket stops
// chaining (e.g. probe requests in v1.1.19).
func collectIEs(pkt gopacket.Packet, frameType layers.Dot11Type) []*ieRecord {
	// Fast path: gopacket already chained the IEs as layers.
	var chained []*ieRecord
	for _, l := range pkt.Layers() {
		if ie, ok := l.(*layers.Dot11InformationElement); ok {
			chained = append(chained, &ieRecord{ID: ie.ID, Info: ie.Info})
		}
	}
	if len(chained) > 0 {
		return chained
	}

	// Fallback: pull raw IE bytes from the Dot11 layer payload.
	dot11Layer := pkt.Layer(layers.LayerTypeDot11)
	if dot11Layer == nil {
		return nil
	}
	body := dot11Layer.LayerPayload()
	if frameType == layers.Dot11TypeMgmtBeacon {
		// Beacon fixed params: timestamp(8) + interval(2) + capability(2) = 12 bytes
		if len(body) < 12 {
			return nil
		}
		body = body[12:]
	}
	return parseRawIEs(body)
}

// ieRecord is a minimal IE representation used internally by the normalize
// package. It mirrors layers.Dot11InformationElement but avoids import cycles
// and lets us fill it from raw bytes without gopacket's layer machinery.
type ieRecord struct {
	ID   layers.Dot11InformationElementID
	Info []byte
}

// parseRawIEs manually walks the raw IE byte stream.
func parseRawIEs(data []byte) []*ieRecord {
	var out []*ieRecord
	for i := 0; i+1 < len(data); {
		id := layers.Dot11InformationElementID(data[i])
		length := int(data[i+1])
		i += 2
		if i+length > len(data) {
			break
		}
		info := make([]byte, length)
		copy(info, data[i:i+length])
		out = append(out, &ieRecord{ID: id, Info: info})
		i += length
	}
	return out
}

// parseRSN inspects the RSN IE body to distinguish WPA2 from WPA3 (SAE AKM).
func parseRSN(data []byte) string {
	// RSN IE: version(2) + group_cipher(4) + pairwise_count(2) + pairwise(N*4)
	//         + akm_count(2) + akms(N*4)
	const hdrLen = 2 + 4 // version + group cipher
	if len(data) < hdrLen+2 {
		return "wpa2"
	}
	offset := hdrLen
	pairwiseCount := int(binary.LittleEndian.Uint16(data[offset:]))
	offset += 2 + pairwiseCount*4
	if len(data) < offset+2 {
		return "wpa2"
	}
	akmCount := int(binary.LittleEndian.Uint16(data[offset:]))
	offset += 2
	for i := 0; i < akmCount; i++ {
		if len(data) < offset+4 {
			break
		}
		// OUI 00:0F:AC, AKM type 8 (SAE) or 12 (FT-SAE)
		if data[offset] == 0x00 && data[offset+1] == 0x0F && data[offset+2] == 0xAC {
			if data[offset+3] == 8 || data[offset+3] == 12 {
				return "wpa3"
			}
		}
		offset += 4
	}
	return "wpa2"
}

// isWPAVendorIE returns true if data is a WPA (WPA1) vendor IE body.
// OUI=00:50:F2, type=01
func isWPAVendorIE(data []byte) bool {
	return len(data) >= 4 &&
		data[0] == 0x00 && data[1] == 0x50 && data[2] == 0xF2 && data[3] == 0x01
}

// freqToChannel converts a radiotap channel frequency (MHz) to an 802.11
// channel number. Returns 0 for unrecognised frequencies.
func freqToChannel(freq uint16) int {
	switch {
	case freq == 2484:
		return 14
	case freq >= 2412 && freq <= 2472:
		return int((freq - 2407) / 5)
	case freq >= 5160 && freq <= 5885:
		return int((freq - 5000) / 5)
	case freq >= 5955 && freq <= 7115: // 6 GHz band (Wi-Fi 6E)
		return int((freq-5950)/5) + 1
	default:
		return 0
	}
}
