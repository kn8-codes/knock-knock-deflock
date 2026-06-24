"""Data models for KKD BI Processor. Shared across all modules."""
from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class CaptureEvent:
    """Mirrors the CaptureEvent struct from kkd-leaf exactly."""
    ts: str
    session_id: str
    type: str           # wifi_beacon | wifi_probe | ble_adv
    mac: str
    vendor: str
    ssid: str
    probe_ssids: list[str]
    rssi: int
    channel: int
    enc: str            # open | wep | wpa | wpa2 | wpa3
    ble_name: str
    ble_mfr: str        # Bluetooth SIG company ID, e.g. "0x004C"
    lat: float
    lon: float
    node_id: str
    iface_id: str


@dataclass
class AP:
    mac: str
    ssid: str
    enc: str
    vendor: str
    channel: int


@dataclass
class BLEDevice:
    mac: str
    vendor: str
    ble_name: str
    ble_mfr: str


@dataclass
class BusinessRecord:
    name: str
    aps: dict[str, AP] = field(default_factory=dict)
    ble_devices: dict[str, BLEDevice] = field(default_factory=dict)
    probe_ssids: set[str] = field(default_factory=set)
    session_ids: set[str] = field(default_factory=set)
    event_count: int = 0
    # Populated by scorer
    score: int = 0
    tier: str = "unknown"
    best_encryption: str = ""
    best_vendor_tier: str = ""
    best_ble_type: str = ""
    # Component scores (for report breakdown)
    score_enc: int = 0
    score_vendor: int = 0
    score_ble: int = 0
    score_density: int = 0


@dataclass
class UnassignedBucket:
    event_count: int = 0
    ssids: set[str] = field(default_factory=set)
    ble_devices: dict[str, BLEDevice] = field(default_factory=dict)
