# KKD BI Processor — Belt.works Plugin B

Reads kkd-leaf NDJSON capture files, groups events by business using a
manual SSID-anchored boundary file, scores infrastructure sophistication,
and outputs a ranked prospect list.

## Usage

```sh
pip install -r requirements.txt

python bi_processor.py \
    --input captures/session1.ndjson captures/session2.ndjson \
    --boundary boundaries/main-corridor.json \
    --output reports/
```

Output: `reports/kkd-bi-YYYY-MM-DD-HHMM.md`

## Boundary file

Maps business names to lists of SSID substrings. Matching is
case-insensitive substring — `"COFFEE"` matches `"COFFEE_5G"` and
`"MY_COFFEE_GUEST"`.

```json
{
  "Coffee Corner":    ["COFFEE", "CORNER_WIFI"],
  "Tech Hub":         ["TECHHUB", "THUB_"],
  "Metro Dental":     ["DENTAL", "METRO_WIFI"]
}
```

Events with no SSID match go into an **Unassigned** bucket at the bottom
of the report. They are never dropped. Unmatched SSIDs are listed to help
fill gaps in the boundary file.

## Scoring model

Score is 0–100. Higher = more sophisticated = higher priority prospect.
Each dimension is an independent function in `scorer.py`.

| Dimension | Max pts | What wins |
|---|---|---|
| Encryption posture | 40 | WPA3=32, WPA2=20, WPA=10, WEP=5, Open=0 |
| AP vendor tier | 30 | managed=30, semi=18, consumer=8, unknown=2 |
| BLE device types | 20 | POS terminal=20, industrial=18, medical=15, consumer=3 |
| Infrastructure density | 10 | ≥3 APs=10, 2 APs=6, 1 AP=3 |

**Vendor tiers** (matched against OUI vendor string from kkd-leaf):
- **Managed (30):** Ubiquiti/UniFi, Meraki, Cisco, Aruba, Ruckus, Extreme, Mist
- **Semi (18):** TP-Link Omada, Amazon Eero Pro, Netgear Orbi/Insight
- **Consumer (8):** Netgear, Linksys, ASUS, Belkin, TP-Link, D-Link
- **Unknown (2):** no vendor string, or OEM default SSID pattern

**Infrastructure tiers** (from total score):
- **managed** ≥ 60
- **semi** ≥ 35
- **consumer** ≥ 15
- **unknown** < 15

**To add a scoring dimension:** add one function `(BusinessRecord) -> int`
to `scorer.py` and include it in `score_business()`. No other files change.

## BLE association

BLE events have no SSID anchor. In v0, BLE devices are associated with a
business when their `session_id` also captured that business's WiFi beacons.
BLE devices whose session has no WiFi match remain in the Unassigned bucket.

## Module layout

| File | Responsibility |
|---|---|
| `bi_processor.py` | CLI, orchestration |
| `models.py` | CaptureEvent, BusinessRecord dataclasses |
| `loader.py` | Parse + validate NDJSON files |
| `boundary.py` | Load boundary JSON, SSID matching |
| `grouper.py` | Assign events to business records |
| `scorer.py` | Four scoring dimension functions |
| `oui_categories.py` | Vendor tier patterns, BLE manufacturer IDs |
| `dashboard.py` | Rich terminal live display |
| `reporter.py` | Markdown file writer |
