package event

import "time"

// CaptureEvent is the canonical output record for all capture types.
// This struct is locked — no field may be removed or retyped.
// Additions are append-only and require a spec amendment.
type CaptureEvent struct {
	Timestamp  time.Time `json:"ts"`
	SessionID  string    `json:"session_id"`
	Type       string    `json:"type"`
	MAC        string    `json:"mac"`
	Vendor     string    `json:"vendor"`
	SSID       string    `json:"ssid"`
	ProbeSSIDs []string  `json:"probe_ssids"`
	RSSI       int       `json:"rssi"`
	Channel    int       `json:"channel"`
	Encryption string    `json:"enc"`
	BLEName    string    `json:"ble_name"`
	BLEMfr     string    `json:"ble_mfr"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	NodeID     string    `json:"node_id"`
	IfaceID    string    `json:"iface_id"`
}
