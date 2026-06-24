#!/usr/bin/env python3
"""
KKD BI Processor — Belt.works Plugin B

Reads kkd-leaf NDJSON capture files, groups events by business using a
manual SSID-anchored boundary file, scores infrastructure sophistication,
and outputs a ranked prospect list.

Usage:
    python bi_processor.py \\
        --input captures/session1.ndjson captures/session2.ndjson \\
        --boundary boundaries/main-corridor.json \\
        --output reports/
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

try:
    from rich import box
    from rich.columns import Columns
    from rich.console import Console
    from rich.live import Live
    from rich.panel import Panel
    from rich.table import Table
    from rich.text import Text
except ImportError:
    print("rich is required: pip install rich", file=sys.stderr)
    sys.exit(1)

console = Console()


# ── Data models ───────────────────────────────────────────────────────────────


@dataclass
class APRecord:
    mac: str
    ssid: str
    enc: str
    vendor: str
    channel: int


@dataclass
class BLERecord:
    mac: str
    vendor: str
    ble_name: str
    ble_mfr: str


@dataclass
class BusinessRecord:
    name: str
    aps: dict[str, APRecord] = field(default_factory=dict)
    ble_devices: dict[str, BLERecord] = field(default_factory=dict)
    probe_ssids: set[str] = field(default_factory=set)
    sessions: set[str] = field(default_factory=set)
    event_count: int = 0
    score: int = 0
    tier: str = "unknown"
    score_enc: int = 0
    score_vendor: int = 0
    score_ble: int = 0
    score_density: int = 0


@dataclass
class UnassignedBucket:
    event_count: int = 0
    ssids: set[str] = field(default_factory=set)
    ble_devices: dict[str, BLERecord] = field(default_factory=dict)


@dataclass
class ProcessingStats:
    files_total: int = 0
    files_loaded: int = 0
    events_total: int = 0
    events_skipped: int = 0


# ── Scoring constants ─────────────────────────────────────────────────────────

# enc field values from kkd-leaf: "open" | "wep" | "wpa" | "wpa2" | "wpa3"
# WPA3-Enterprise (score 40) is indistinguishable from WPA3-Personal in v0.
ENC_SCORES: dict[str, int] = {
    "wpa3": 32,
    "wpa2": 20,
    "wpa":  10,
    "wep":   5,
    "open":  0,
}

# Substrings matched against ap.vendor (case-insensitive)
_MANAGED_VENDOR_PATS  = ("ubiquiti", "unifi", "meraki", "cisco", "aruba", "ruckus")
_SEMI_VENDOR_PATS     = ("omada", "eero", "insight")
_CONSUMER_VENDOR_PATS = ("netgear", "linksys", "asus", "belkin", "tp-link", "tplink")

# Default SSID patterns → unconfigured / OEM firmware
_DEFAULT_SSID_RE = re.compile(
    r"^(NETGEAR|Linksys|ASUS_|ASUS |BelkinN|belkin\.|xfinitywifi|ATT|XFINITY|"
    r"default|HUAWEI-|FRITZ!Box|dlink|D-Link)",
    re.IGNORECASE,
)

# BLE device classification — matched against ble_name and vendor (case-insensitive)
_POS_PATS     = ("square", "clover", "verifone", "ingenico", "pax ")
_PRINTER_PATS = ("zebra", "honeywell", "brother", "dymo", "bixolon")
_MEDICAL_PATS = ("philips", "ge healthcare", "natus", "welch allyn", "masimo", "nihon")
# Consumer device Bluetooth company IDs (0x004C = Apple)
_CONSUMER_BLE_MFRS = {"0x004C", "0x0075", "0x00E0"}  # Apple, Samsung, Google

TIER_COLORS = {
    "managed":  "bright_green",
    "semi":     "yellow",
    "consumer": "cyan",
    "unknown":  "dim",
}

ENC_ORDER = ["wpa3", "wpa2", "wpa", "wep", "open"]


# ── Scoring ───────────────────────────────────────────────────────────────────


def _enc_score(record: BusinessRecord) -> int:
    best = 0
    for ap in record.aps.values():
        best = max(best, ENC_SCORES.get(ap.enc, 0))
    return min(best, 40)


def _vendor_score(record: BusinessRecord) -> int:
    best = 0
    for ap in record.aps.values():
        v = (ap.vendor or "").lower()
        ssid = ap.ssid or ""

        if any(p in v for p in _MANAGED_VENDOR_PATS):
            best = max(best, 30)
        elif any(p in v for p in _SEMI_VENDOR_PATS):
            best = max(best, 18)
        elif any(p in v for p in _CONSUMER_VENDOR_PATS):
            best = max(best, 8)
        elif v:
            best = max(best, 8)  # known vendor not in any tier
        elif _DEFAULT_SSID_RE.match(ssid):
            best = max(best, 2)  # SSID looks like OEM default
        else:
            best = max(best, 2)  # no vendor info at all
    return min(best, 30)


def _ble_score(record: BusinessRecord) -> int:
    best = 0
    for ble in record.ble_devices.values():
        n = (ble.ble_name or "").lower()
        v = (ble.vendor or "").lower()
        mfr = (ble.ble_mfr or "").upper()
        combined = n + " " + v

        if any(p in combined for p in _POS_PATS):
            best = max(best, 20)
        elif any(p in combined for p in _PRINTER_PATS):
            best = max(best, 18)
        elif any(p in combined for p in _MEDICAL_PATS):
            best = max(best, 15)
        elif mfr in _CONSUMER_BLE_MFRS:
            best = max(best, 3)
        else:
            best = max(best, 1)  # any BLE device = lowest signal
    return min(best, 20)


def _density_score(record: BusinessRecord) -> int:
    n = len(record.aps)
    if n >= 3:
        return 10
    if n == 2:
        return 6
    if n == 1:
        return 3
    return 0


def score_business(record: BusinessRecord) -> None:
    record.score_enc     = _enc_score(record)
    record.score_vendor  = _vendor_score(record)
    record.score_ble     = _ble_score(record)
    record.score_density = _density_score(record)
    total = record.score_enc + record.score_vendor + record.score_ble + record.score_density
    record.score = min(100, total)

    if record.score >= 60:
        record.tier = "managed"
    elif record.score >= 35:
        record.tier = "semi"
    elif record.score >= 15:
        record.tier = "consumer"
    else:
        record.tier = "unknown"


# ── SSID matching ─────────────────────────────────────────────────────────────


def build_boundary_index(boundary: dict[str, list[str]]) -> list[tuple[str, str]]:
    """Returns flat [(pattern_lower, business_name)] for linear scan matching."""
    index = []
    for biz_name, patterns in boundary.items():
        for pattern in patterns:
            index.append((pattern.lower(), biz_name))
    return index


def _find_business(ssid: str, index: list[tuple[str, str]]) -> str | None:
    if not ssid:
        return None
    ssid_lower = ssid.lower()
    for pattern, biz_name in index:
        if pattern in ssid_lower:
            return biz_name
    return None


def _get_or_create(name: str, businesses: dict[str, BusinessRecord]) -> BusinessRecord:
    if name not in businesses:
        businesses[name] = BusinessRecord(name=name)
    return businesses[name]


# ── Event assignment ──────────────────────────────────────────────────────────


def assign_event(
    event: dict,
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    index: list[tuple[str, str]],
    ble_by_session: dict[str, list[dict]],
) -> None:
    etype = event.get("type", "")
    mac = event.get("mac", "")
    session_id = event.get("session_id", "")

    if etype == "wifi_beacon":
        ssid = event.get("ssid", "")
        biz_name = _find_business(ssid, index)
        if biz_name:
            biz = _get_or_create(biz_name, businesses)
            biz.event_count += 1
            biz.sessions.add(session_id)
            biz.aps[mac] = APRecord(
                mac=mac,
                ssid=ssid,
                enc=event.get("enc", ""),
                vendor=event.get("vendor", ""),
                channel=event.get("channel", 0),
            )
        else:
            unassigned.event_count += 1
            if ssid:
                unassigned.ssids.add(ssid)

    elif etype == "wifi_probe":
        probe_ssids = [s for s in event.get("probe_ssids", []) if s]
        biz_name = None
        for ssid in probe_ssids:
            biz_name = _find_business(ssid, index)
            if biz_name:
                break

        if biz_name:
            biz = _get_or_create(biz_name, businesses)
            biz.event_count += 1
            biz.sessions.add(session_id)
            biz.probe_ssids.update(probe_ssids)
        else:
            unassigned.event_count += 1
            unassigned.ssids.update(probe_ssids)

    elif etype == "ble_adv":
        # BLE events are collected by session; associated with businesses after
        # WiFi assignment determines which sessions map to which businesses.
        ble_by_session.setdefault(session_id, []).append(event)
        unassigned.event_count += 1
        unassigned.ble_devices[mac] = BLERecord(
            mac=mac,
            vendor=event.get("vendor", ""),
            ble_name=event.get("ble_name", ""),
            ble_mfr=event.get("ble_mfr", ""),
        )


def associate_ble_by_session(
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    ble_by_session: dict[str, list[dict]],
) -> None:
    """Promote BLE devices to businesses when their session also captured that
    business's WiFi. A BLE device seen in multiple matching sessions is added
    to all matching businesses (it's in the same corridor).
    """
    session_to_biz: dict[str, list[str]] = {}
    for biz_name, rec in businesses.items():
        for sid in rec.sessions:
            session_to_biz.setdefault(sid, []).append(biz_name)

    for session_id, events in ble_by_session.items():
        biz_names = session_to_biz.get(session_id, [])
        if not biz_names:
            continue
        for event in events:
            mac = event.get("mac", "")
            ble = BLERecord(
                mac=mac,
                vendor=event.get("vendor", ""),
                ble_name=event.get("ble_name", ""),
                ble_mfr=event.get("ble_mfr", ""),
            )
            for biz_name in biz_names:
                businesses[biz_name].ble_devices[mac] = ble
            # Remove from unassigned — it's been promoted
            unassigned.ble_devices.pop(mac, None)
            unassigned.event_count -= 1


# ── File loading ──────────────────────────────────────────────────────────────


def load_ndjson(path: Path) -> tuple[list[dict], int]:
    """Returns (events, skipped_count)."""
    events: list[dict] = []
    skipped = 0
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                skipped += 1
    return events, skipped


# ── Terminal dashboard ────────────────────────────────────────────────────────


def _best_enc(record: BusinessRecord) -> str:
    seen = {ap.enc for ap in record.aps.values()}
    for e in ENC_ORDER:
        if e in seen:
            return e
    return "—"


def _ap_vendors(record: BusinessRecord) -> str:
    vendors = sorted({ap.vendor for ap in record.aps.values() if ap.vendor})
    if not vendors:
        return "unknown"
    return ", ".join(v.split()[0] for v in vendors[:3])  # first word, max 3


def _ble_summary(record: BusinessRecord) -> str:
    if not record.ble_devices:
        return "—"
    names = []
    for ble in record.ble_devices.values():
        label = ble.ble_name or ble.vendor or "unknown"
        names.append(label)
    unique = sorted(set(names))
    return ", ".join(unique[:3]) + ("…" if len(unique) > 3 else "")


def make_stats_panel(
    stats: ProcessingStats,
    biz_count: int,
    unassigned: UnassignedBucket,
) -> Panel:
    lines = [
        f"Files   : [bold]{stats.files_loaded}[/bold] / {stats.files_total}",
        f"Events  : [bold]{stats.events_total:,}[/bold]",
        f"Businesses : [bold]{biz_count}[/bold]",
        f"Unassigned : [bold]{unassigned.event_count:,}[/bold]",
    ]
    if stats.events_skipped:
        lines.append(f"[yellow]Bad JSON: {stats.events_skipped}[/yellow]")
    return Panel("\n".join(lines), title="[bold blue]Processing[/bold blue]", padding=(0, 1))


def make_top5_table(businesses: dict[str, BusinessRecord]) -> Table:
    ranked = sorted(businesses.values(), key=lambda b: b.score, reverse=True)[:5]
    t = Table(
        title="[bold]Top 5 Prospects[/bold]",
        box=box.SIMPLE_HEAVY,
        show_header=True,
        header_style="bold",
        min_width=60,
    )
    t.add_column("#",        style="dim", width=3, no_wrap=True)
    t.add_column("Business", min_width=22, no_wrap=True)
    t.add_column("Score",    justify="right", width=8)
    t.add_column("Tier",     width=10)
    t.add_column("APs",      justify="right", width=4)
    t.add_column("Enc",      width=6)

    for i, rec in enumerate(ranked, 1):
        color = TIER_COLORS.get(rec.tier, "white")
        t.add_row(
            str(i),
            rec.name,
            f"[{color}]{rec.score}/100[/{color}]",
            f"[{color}]{rec.tier}[/{color}]",
            str(len(rec.aps)),
            _best_enc(rec),
        )
    if not ranked:
        t.add_row("—", "[dim]no data yet[/dim]", "—", "—", "—", "—")
    return t


def make_live_view(
    stats: ProcessingStats,
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
) -> Columns:
    return Columns(
        [make_stats_panel(stats, len(businesses), unassigned), make_top5_table(businesses)],
        equal=False,
        expand=True,
    )


# ── Markdown report ───────────────────────────────────────────────────────────


def _probe_ssid_list(record: BusinessRecord) -> str:
    ssids = sorted(record.probe_ssids)
    if not ssids:
        return "—"
    return ", ".join(f'"{s}"' for s in ssids[:10]) + ("…" if len(ssids) > 10 else "")


def write_markdown_report(
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    stats: ProcessingStats,
    output_dir: Path,
) -> Path:
    now = datetime.now(timezone.utc)
    filename = f"kkd-bi-{now.strftime('%Y-%m-%d-%H%M')}.md"
    out_path = output_dir / filename

    ranked = sorted(businesses.values(), key=lambda b: b.score, reverse=True)
    total_sessions = len({sid for rec in ranked for sid in rec.sessions})

    lines: list[str] = [
        f"# KKD BI Report — {now.strftime('%Y-%m-%d %H:%M UTC')}",
        "",
        f"Generated by Belt.works BI Processor (kkd-leaf Plugin B)  ",
        f"Sessions: {total_sessions} | Total events: {stats.events_total:,} | "
        f"Businesses identified: {len(ranked)}",
        "",
        "---",
        "",
    ]

    for rank, rec in enumerate(ranked, 1):
        lines += [
            f"## {rank}. {rec.name} — Score: {rec.score}/100 ({rec.tier})",
            "",
            "| Field | Value |",
            "|---|---|",
            f"| Sessions | {len(rec.sessions)} |",
            f"| APs | {len(rec.aps)} |",
            f"| Best encryption | {_best_enc(rec)} |",
            f"| AP vendors | {_ap_vendors(rec)} |",
            f"| BLE devices | {_ble_summary(rec)} |",
            f"| Client probes | {len(rec.probe_ssids)} unique SSIDs |",
            f"| Probe SSIDs seen | {_probe_ssid_list(rec)} |",
            "",
            "<details><summary>Score breakdown</summary>",
            "",
            f"- Encryption posture: {rec.score_enc}/40",
            f"- AP vendor tier:     {rec.score_vendor}/30",
            f"- BLE device types:   {rec.score_ble}/20",
            f"- Infrastructure density: {rec.score_density}/10",
            "",
            "</details>",
            "",
            "---",
            "",
        ]

    # Unassigned section
    lines += [
        "## Unassigned",
        "",
        f"Events: {unassigned.event_count:,} | BLE devices: {len(unassigned.ble_devices)}",
        "",
    ]

    unmatched_ssids = sorted(unassigned.ssids)
    if unmatched_ssids:
        lines.append(
            "Unique SSIDs not matched to any business "
            "(consider adding to boundary file):"
        )
        lines.append("")
        for ssid in unmatched_ssids[:50]:
            lines.append(f"- `{ssid}`")
        if len(unmatched_ssids) > 50:
            lines.append(f"- … and {len(unmatched_ssids) - 50} more")
        lines.append("")

    if unassigned.ble_devices:
        lines.append("Unassigned BLE devices:")
        lines.append("")
        lines.append("| MAC | Vendor | Name | Mfr |")
        lines.append("|---|---|---|---|")
        for ble in list(unassigned.ble_devices.values())[:30]:
            lines.append(
                f"| {ble.mac} | {ble.vendor or '—'} | {ble.ble_name or '—'} | {ble.ble_mfr or '—'} |"
            )
        if len(unassigned.ble_devices) > 30:
            lines.append(f"| … | {len(unassigned.ble_devices) - 30} more | | |")

    out_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return out_path


# ── Main ──────────────────────────────────────────────────────────────────────


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="KKD BI Processor — Belt.works Plugin B",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument(
        "--input", "-i",
        nargs="+",
        required=True,
        metavar="FILE.ndjson",
        help="One or more kkd-leaf NDJSON capture files",
    )
    p.add_argument(
        "--boundary", "-b",
        required=True,
        metavar="BOUNDARY.json",
        help="Business boundary file (JSON: {name: [ssid_patterns]})",
    )
    p.add_argument(
        "--output", "-o",
        default=".",
        metavar="DIR",
        help="Directory for the output markdown report (default: current dir)",
    )
    return p.parse_args()


def main() -> None:
    args = parse_args()

    # Validate inputs
    input_paths = [Path(p) for p in args.input]
    for p in input_paths:
        if not p.exists():
            console.print(f"[red]ERROR:[/red] input file not found: {p}")
            sys.exit(1)

    boundary_path = Path(args.boundary)
    if not boundary_path.exists():
        console.print(f"[red]ERROR:[/red] boundary file not found: {boundary_path}")
        sys.exit(1)

    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Load boundary file
    try:
        boundary: dict[str, list[str]] = json.loads(boundary_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError) as exc:
        console.print(f"[red]ERROR:[/red] cannot read boundary file: {exc}")
        sys.exit(1)

    index = build_boundary_index(boundary)
    console.print(
        f"[bold]Boundary:[/bold] {len(boundary)} business(es), "
        f"{len(index)} SSID pattern(s)"
    )

    # Processing state
    businesses: dict[str, BusinessRecord] = {}
    unassigned = UnassignedBucket()
    ble_by_session: dict[str, list[dict]] = {}
    stats = ProcessingStats(files_total=len(input_paths))

    with Live(
        make_live_view(stats, businesses, unassigned),
        console=console,
        refresh_per_second=4,
        transient=False,
    ) as live:
        for path in input_paths:
            events, skipped = load_ndjson(path)
            stats.files_loaded += 1
            stats.events_total += len(events)
            stats.events_skipped += skipped

            for event in events:
                assign_event(event, businesses, unassigned, index, ble_by_session)

            # Score after each file so the top-5 updates live
            for rec in businesses.values():
                score_business(rec)

            live.update(make_live_view(stats, businesses, unassigned))

        # Final BLE association pass
        associate_ble_by_session(businesses, unassigned, ble_by_session)

        # Re-score now that BLE devices are associated
        for rec in businesses.values():
            score_business(rec)

        live.update(make_live_view(stats, businesses, unassigned))

    # Final ranked table to stdout
    ranked = sorted(businesses.values(), key=lambda b: b.score, reverse=True)
    final_table = Table(
        title=f"\n[bold]Final Rankings[/bold] — {len(ranked)} business(es)",
        box=box.ROUNDED,
        show_header=True,
        header_style="bold",
    )
    final_table.add_column("#",          style="dim", width=3)
    final_table.add_column("Business",   min_width=22)
    final_table.add_column("Score",      justify="right", width=8)
    final_table.add_column("Tier",       width=10)
    final_table.add_column("APs",        justify="right", width=5)
    final_table.add_column("Enc",        width=6)
    final_table.add_column("Vendors",    min_width=18)
    final_table.add_column("BLE",        width=6, justify="right")
    final_table.add_column("Probes",     width=7, justify="right")

    for i, rec in enumerate(ranked, 1):
        color = TIER_COLORS.get(rec.tier, "white")
        final_table.add_row(
            str(i),
            rec.name,
            f"[{color}]{rec.score}/100[/{color}]",
            f"[{color}]{rec.tier}[/{color}]",
            str(len(rec.aps)),
            _best_enc(rec),
            _ap_vendors(rec),
            str(len(rec.ble_devices)),
            str(len(rec.probe_ssids)),
        )
    console.print(final_table)

    if unassigned.event_count:
        console.print(
            f"[dim]Unassigned: {unassigned.event_count:,} events, "
            f"{len(unassigned.ssids)} unique SSIDs, "
            f"{len(unassigned.ble_devices)} BLE devices[/dim]"
        )

    # Write markdown report
    report_path = write_markdown_report(businesses, unassigned, stats, output_dir)
    console.print(f"\n[bold green]Report written:[/bold green] {report_path}")


if __name__ == "__main__":
    main()
