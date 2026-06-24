#!/usr/bin/env python3
"""
KKD BI Processor — Belt.works Plugin B
Entry point and orchestration.

Usage:
    python bi_processor.py \\
        --input captures/s1.ndjson captures/s2.ndjson \\
        --boundary boundaries/main-corridor.json \\
        --output reports/
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

try:
    from rich.console import Console
    from rich.live import Live
except ImportError:
    print("rich is required: pip install rich", file=sys.stderr)
    sys.exit(1)

from boundary import build_index, load_boundary
from dashboard import make_final_table, make_live_display
from grouper import group_events
from loader import load_files
from reporter import write_report
from scorer import score_business

console = Console()


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
        help="Business boundary file",
    )
    p.add_argument(
        "--output", "-o",
        default="./reports/",
        metavar="DIR",
        help="Output directory for the markdown report (default: ./reports/)",
    )
    return p.parse_args()


def main() -> None:
    args = parse_args()

    input_paths = [Path(p) for p in args.input]
    boundary_path = Path(args.boundary)
    output_dir = Path(args.output)

    for p in input_paths:
        if not p.exists():
            console.print(f"[red]ERROR:[/red] input file not found: {p}")
            sys.exit(1)
    if not boundary_path.exists():
        console.print(f"[red]ERROR:[/red] boundary file not found: {boundary_path}")
        sys.exit(1)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Load boundary
    boundary = load_boundary(boundary_path)
    index = build_index(boundary)
    console.print(
        f"[bold]Boundary:[/bold] {len(boundary)} business(es), "
        f"{len(index)} SSID pattern(s)"
    )

    # Load capture files
    console.print(f"Loading {len(input_paths)} file(s)...")
    events, skipped = load_files(input_paths)
    if skipped:
        console.print(f"  [yellow]{skipped} line(s) skipped (bad JSON or missing fields)[/yellow]")
    console.print(f"  {len(events):,} events ready")

    # Group events into business records
    businesses, unassigned = group_events(events, index)

    # Score each business with live dashboard updating
    names = list(businesses.keys())
    with Live(
        make_live_display(businesses, unassigned, len(events), str(output_dir)),
        console=console,
        refresh_per_second=10,
        transient=False,
    ) as live:
        for name in names:
            score_business(businesses[name])
            live.update(
                make_live_display(businesses, unassigned, len(events), str(output_dir))
            )

    # Final ranked table
    console.print(make_final_table(businesses))

    if unassigned.event_count:
        console.print(
            f"\n[dim]Unassigned: {unassigned.event_count:,} events · "
            f"{len(unassigned.ssids)} SSID(s) · "
            f"{len(unassigned.ble_devices)} BLE device(s)[/dim]"
        )

    # Write markdown report
    report_path = write_report(businesses, unassigned, len(events), output_dir)
    console.print(f"\n[bold green]Report written:[/bold green] {report_path}")


if __name__ == "__main__":
    main()
