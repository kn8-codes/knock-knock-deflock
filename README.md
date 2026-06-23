# knock-knock-deflock

Knock-knock-deflock (KKD) is a passive RF capture platform that maps WiFi access points and BLE devices along routes walked by canvassers. The open-source core (`kkd-leaf`) runs on low-cost Raspberry Pi hardware and writes structured NDJSON that three first-party plugins consume: **DeFlock** for surveillance infrastructure mapping, **Belt.works** for operational BI, and **FieldSignal** for campaign intelligence.

---

## What it captures

| Signal | Fields |
|--------|--------|
| WiFi beacon | MAC, vendor (OUI), SSID, encryption posture (open/wep/wpa/wpa2/wpa3), RSSI, channel |
| WiFi probe request | MAC, vendor, probe SSID list, RSSI, channel |
| BLE advertising | MAC, vendor, device name (from AD type 0x08/0x09), manufacturer ID (AD type 0xFF), RSSI |

## What it does NOT do

- No payload capture. No TCP/IP, no application data, no user content.
- No network association. The WiFi interface stays in passive monitor mode; no connections are made.
- No active probing or injection. BLE is receive-only (passive scan, type 0x00).
- No data frames. Only 802.11 management frames (beacon, probe request) are decoded; data frames are dropped.

This is passive broadcast capture — the same legal basis as Street View wardriving.

---

## Build

Requires Go 1.21+. All targets cross-compile from any host with `CGO_ENABLED=0`.

```sh
bash scripts/build-all.sh
```

Produces four static binaries:

| Binary | Target |
|--------|--------|
| `kkd-leaf-linux-arm6` | Pi Zero W (ARMv6) |
| `kkd-leaf-linux-arm7` | Pi 3B+ (ARMv7) |
| `kkd-leaf-linux-arm64` | Pi 4B, Orange Pi (ARM64) |
| `kkd-leaf-linux-amd64` | x86\_64 |

---

## Deploy

```sh
scp kkd-leaf-linux-arm64 pi@192.168.1.10:/usr/local/bin/kkd-leaf
ssh pi@192.168.1.10 'chmod +x /usr/local/bin/kkd-leaf'
```

---

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `KKD_NODE_ID` | hostname | Identifies this device in every event |
| `KKD_WIFI_IFACES` | _(none)_ | Comma-separated monitor-mode WiFi interfaces, e.g. `wlan0` |
| `KKD_BLE_IFACES` | _(none)_ | Comma-separated HCI devices, e.g. `hci0` |
| `KKD_OUTPUT_FILE` | `./kkd-capture.ndjson` | Output file path; rotated in place |
| `KKD_ROTATE_SIZE` | `104857600` | Rotate when file reaches this size in bytes (default 100 MB) |
| `KKD_ROTATE_AGE` | `86400s` | Rotate when file reaches this age (default 24 h) |
| `KKD_ROTATE_KEEP` | `7` | Number of completed rotation files to retain |
| `KKD_STATS_INTERVAL` | `60s` | How often per-interface counters are logged to stderr |

At least one of `KKD_WIFI_IFACES` or `KKD_BLE_IFACES` must be set.

---

## Run

`kkd-leaf` requires **root** (or `CAP_NET_RAW`) to open raw AF\_PACKET and AF\_BLUETOOTH/HCI sockets.

For WiFi capture the interface must be placed in **monitor mode** before launch (the daemon does this automatically via `iw`):

```sh
sudo KKD_NODE_ID=pi-zero-01 \
     KKD_WIFI_IFACES=wlan0 \
     KKD_BLE_IFACES=hci0 \
     KKD_OUTPUT_FILE=/data/capture.ndjson \
     /usr/local/bin/kkd-leaf
```

Send `SIGTERM` or `SIGINT` to stop cleanly. The daemon restores the WiFi interface to managed mode on exit.

---

## Output format

Each line is a JSON object conforming to `CaptureEvent` (`internal/event/event.go`). Fields unused by a given capture type (e.g. `ssid` on a BLE event) are always present as zero values.

```json
{"ts":"2026-06-23T14:30:01Z","session_id":"...","type":"wifi_beacon","mac":"AA:BB:CC:DD:EE:FF","vendor":"Raspberry Pi Trading","ssid":"HomeNet","probe_ssids":[],"rssi":-67,"channel":6,"enc":"wpa2","ble_name":"","ble_mfr":"","lat":0,"lon":0,"node_id":"pi-zero-01","iface_id":"wlan0"}
```

---

## License

MIT
