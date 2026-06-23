package normalize

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

// mustHex decodes a hex string, collapsing whitespace first.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	s = strings.ReplaceAll(s, " ", "")
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	return b
}

// makePacket wraps raw bytes in a gopacket.Packet using IEEE802.11Radio framing.
func makePacket(t *testing.T, raw []byte) gopacket.Packet {
	t.Helper()
	return gopacket.NewPacket(raw, layers.LinkTypeIEEE80211Radio, gopacket.Default)
}

// stubDB returns an OUI db loaded from a tiny inline manuf snippet.
func stubDB(t *testing.T) *oui.DB {
	t.Helper()
	db, err := oui.LoadBytes([]byte("AA:BB:CC\tTestCo\tTest Company Ltd\n"))
	if err != nil {
		t.Fatalf("oui.LoadBytes: %v", err)
	}
	return db
}

// --- freqToChannel ---

func TestFreqToChannel(t *testing.T) {
	cases := []struct {
		freq uint16
		want int
	}{
		{2412, 1},
		{2437, 6},
		{2462, 11},
		{2484, 14},
		{5180, 36},
		{5745, 149},
		{5825, 165},
		{0, 0},
		{3000, 0},
	}
	for _, tc := range cases {
		got := freqToChannel(tc.freq)
		if got != tc.want {
			t.Errorf("freqToChannel(%d) = %d, want %d", tc.freq, got, tc.want)
		}
	}
}

// --- parseRSN ---

func TestParseRSN(t *testing.T) {
	// WPA2: RSN IE body with CCMP pairwise, PSK AKM (00:0F:AC:02)
	wpa2 := mustHex(t,
		"0100"+ // version
			"000FAC04"+ // group cipher: CCMP
			"0100"+"000FAC04"+ // 1 pairwise: CCMP
			"0100"+"000FAC02") // 1 AKM: PSK → WPA2
	if got := parseRSN(wpa2); got != "wpa2" {
		t.Errorf("parseRSN(wpa2) = %q, want \"wpa2\"", got)
	}

	// WPA3: same but AKM type = 08 (SAE)
	wpa3 := mustHex(t,
		"0100"+"000FAC04"+"0100"+"000FAC04"+"0100"+"000FAC08")
	if got := parseRSN(wpa3); got != "wpa3" {
		t.Errorf("parseRSN(wpa3) = %q, want \"wpa3\"", got)
	}

	// Too short — should not panic
	if got := parseRSN([]byte{0x01}); got != "wpa2" {
		t.Errorf("parseRSN(short) = %q, want \"wpa2\"", got)
	}
}

// --- isWPAVendorIE ---

func TestIsWPAVendorIE(t *testing.T) {
	wpa := []byte{0x00, 0x50, 0xF2, 0x01, 0x01, 0x00}
	if !isWPAVendorIE(wpa) {
		t.Error("expected WPA vendor IE to match")
	}
	notWPA := []byte{0x00, 0x50, 0xF2, 0x02}
	if isWPAVendorIE(notWPA) {
		t.Error("type=02 should not match WPA IE")
	}
}

// --- ParseWiFiPacket with a minimal synthetic beacon ---
//
// We build a minimal radiotap + 802.11 beacon byte sequence by hand.
// Frame structure:
//   Radiotap header (signal=-70 dBm, freq=2437 MHz [ch6])
//   802.11 frame control (beacon: type=0 subtype=8)
//   Addresses + seq
//   Beacon fixed params (timestamp + interval + capability)
//   IE 0: SSID "TestNet"
//   IE 3: DS Param channel=6
func TestParseBeacon(t *testing.T) {
	// Radiotap header (24 bytes):
	//   version=0, pad=0, len=0x0018 (24)
	//   present=0x0000_1006 → rate(bit1), dbm_signal(bit5), channel(bit3)
	//   Actually use a real minimal radiotap with TSFT present=0x0000_0006 (bit1=rate, bit2... )
	//   Simplest: present = 0x00008006 → bits: TSFT(bit0 unused), rate(1), channel(3), dbm_signal(5)
	// Let's just build the hex precisely.
	//
	// Minimal radiotap with signal + channel:
	//   0x00 version
	//   0x00 pad
	//   0x1a 0x00 length = 26
	//   present = 0x0000_0806 (rate=bit1, channel=bit3, dbm_signal=bit5 -> 0b 0000 1000 0000 0110 ... wait
	//   bit mapping: bit0=TSFT, bit1=Flags, bit2=Rate, bit3=Channel, bit4=FHSS, bit5=dBmSignal
	//   For channel+signal: present = (1<<3)|(1<<5) = 0x28 -> 0x00 0x00 0x00 0x28
	//   Radiotap: ver(1) pad(1) len(2) present(4) = 8 bytes header overhead
	//   Then: Channel field = freq(2)+flags(2) = 4 bytes; dBmSignal = 1 byte; padding to align?
	//   Channel must be 4-byte aligned from start: offset 8 → already 8-byte aligned ✓
	//   dBmSignal at offset 12, no alignment needed.
	//   Total radiotap = 8 + 4 + 1 = 13 bytes ... but len is 2-byte aligned. Let's use 14.
	//
	// This is getting complex; use a known-good captured radiotap header with no fields
	// and rely on gopacket to handle it. For the test, channel=0/rssi=0 is acceptable
	// since we care about SSID and type parsing more than exact radio metadata.

	// Minimal valid radiotap header: no fields present.
	//   version=0, pad=0, len=8 (header only), present=0x00000000
	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}

	// 802.11 frame control: beacon = type 0 (management), subtype 8
	// FC word: subtype(4)|type(2)|00(2) | flags = 0x80 0x00
	fc := []byte{0x80, 0x00}

	// Duration (2 bytes)
	dur := []byte{0x00, 0x00}

	// Addresses: dst=broadcast, src=BSSID, bssid=BSSID
	bssid := []byte{0xAA, 0xBB, 0xCC, 0x11, 0x22, 0x33}
	bcast := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	addr := append(bcast, bssid...)
	addr = append(addr, bssid...)

	// Sequence control (2 bytes)
	seq := []byte{0x00, 0x00}

	// Beacon fixed params: timestamp(8) + interval(2) + capability(2)
	// capability = 0x0431: ESS|privacy(WEP for test → 0x0011) ... use 0x0001 (open)
	fixedParams := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // timestamp
		0x64, 0x00, // beacon interval = 100 TU
		0x01, 0x00, // capability: ESS set, Privacy not set (open)
	}

	// IE 0: SSID = "TestNet"
	ssid := "TestNet"
	ie0 := append([]byte{0x00, byte(len(ssid))}, []byte(ssid)...)

	// IE 3: DS Param Set, channel = 6
	ie3 := []byte{0x03, 0x01, 0x06}

	frame := radiotap
	frame = append(frame, fc...)
	frame = append(frame, dur...)
	frame = append(frame, addr...)
	frame = append(frame, seq...)
	frame = append(frame, fixedParams...)
	frame = append(frame, ie0...)
	frame = append(frame, ie3...)

	db := stubDB(t)
	pkt := makePacket(t, frame)
	ev := ParseWiFiPacket(pkt, "sess-1", "node-1", "wlan0", db)
	if ev == nil {
		t.Fatal("ParseWiFiPacket returned nil for beacon frame")
	}
	if ev.Type != "wifi_beacon" {
		t.Errorf("Type = %q, want \"wifi_beacon\"", ev.Type)
	}
	if ev.SSID != ssid {
		t.Errorf("SSID = %q, want %q", ev.SSID, ssid)
	}
	if ev.MAC != "aa:bb:cc:11:22:33" && ev.MAC != "AA:BB:CC:11:22:33" {
		t.Errorf("MAC = %q, unexpected", ev.MAC)
	}
	if ev.Encryption != "open" {
		t.Errorf("Encryption = %q, want \"open\"", ev.Encryption)
	}
	if ev.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want \"sess-1\"", ev.SessionID)
	}
	if ev.ProbeSSIDs == nil {
		t.Error("ProbeSSIDs must not be nil on a beacon event")
	}
}

func TestParseProbeReq(t *testing.T) {
	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}

	// Frame control: probe request = type 0 subtype 4 = 0x40 0x00
	fc := []byte{0x40, 0x00}
	dur := []byte{0x00, 0x00}

	client := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	bcast := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	addr := append(bcast, client...)
	addr = append(addr, bcast...)
	seq := []byte{0x00, 0x00}

	// IE 0: specific SSID
	ssid := "HomeNetwork"
	ie0 := append([]byte{0x00, byte(len(ssid))}, []byte(ssid)...)

	frame := radiotap
	frame = append(frame, fc...)
	frame = append(frame, dur...)
	frame = append(frame, addr...)
	frame = append(frame, seq...)
	frame = append(frame, ie0...)

	db := stubDB(t)
	pkt := makePacket(t, frame)
	ev := ParseWiFiPacket(pkt, "sess-2", "node-1", "wlan0", db)
	if ev == nil {
		t.Fatal("ParseWiFiPacket returned nil for probe request")
	}
	if ev.Type != "wifi_probe" {
		t.Errorf("Type = %q, want \"wifi_probe\"", ev.Type)
	}
	if len(ev.ProbeSSIDs) != 1 || ev.ProbeSSIDs[0] != ssid {
		t.Errorf("ProbeSSIDs = %v, want [%q]", ev.ProbeSSIDs, ssid)
	}
}

func TestParseWildcardProbe(t *testing.T) {
	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	fc := []byte{0x40, 0x00}
	dur := []byte{0x00, 0x00}
	client := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	bcast := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	addr := append(bcast, client...)
	addr = append(addr, bcast...)
	seq := []byte{0x00, 0x00}

	// IE 0: empty SSID (wildcard)
	ie0 := []byte{0x00, 0x00}

	frame := radiotap
	frame = append(frame, fc...)
	frame = append(frame, dur...)
	frame = append(frame, addr...)
	frame = append(frame, seq...)
	frame = append(frame, ie0...)

	db := stubDB(t)
	pkt := makePacket(t, frame)
	ev := ParseWiFiPacket(pkt, "sess-3", "node-1", "wlan0", db)
	if ev == nil {
		t.Fatal("ParseWiFiPacket returned nil for wildcard probe")
	}
	if len(ev.ProbeSSIDs) != 0 {
		t.Errorf("ProbeSSIDs = %v, want []", ev.ProbeSSIDs)
	}
}

func TestParseIgnoresDataFrames(t *testing.T) {
	radiotap := []byte{0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00}
	// Data frame: type=2, subtype=0 → FC = 0x08 0x00
	fc := []byte{0x08, 0x00}
	frame := append(radiotap, fc...)
	frame = append(frame, make([]byte, 20)...)

	db := stubDB(t)
	pkt := makePacket(t, frame)
	ev := ParseWiFiPacket(pkt, "s", "n", "w", db)
	if ev != nil {
		t.Errorf("expected nil for data frame, got type=%q", ev.Type)
	}
}
