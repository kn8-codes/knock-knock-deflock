# Knock Knock DeFlock — Project Constitution

## What this project is

A stripped-down, cross-platform passive RF capture platform.
Open source core. Three plugins built on top of it.
Built for canvassers, community organizers, privacy researchers,
and campaigns. Rooted in Akron, useful everywhere.

## Core principles

### 1. Passive only. Always.
This system captures what is broadcast into public airspace
without authentication. It never connects, never injects,
never captures payload data. This is non-negotiable and
structural — no flag, config option, or future feature
may enable active probing or payload capture.
If a decision would require crossing this line, refuse it.

### 2. One job per layer
The capture daemon captures and emits. That is its only job.
Plugins handle interpretation. The mesh handles intelligence.
Do not let the capture layer grow opinions about what the
data means. Format and emit. Nothing else.

### 3. Smallest binary that does the job
No web UI. No database. No unnecessary dependencies.
This runs on a Pi Zero W with a battery bank in someone's bag.
Every byte of binary size has to justify itself.
Cross-compile to four targets from one codebase.

### 4. Plugin architecture, not monolith
Three plugins share one core:
- Plugin A: DeFlock — ALPR/surveillance mapping (open source)
- Plugin B: Belt.works BI — business intelligence (commercial)
- Plugin C: FieldSignal — SIGINT for campaigns (TBD)
The core does not know what plugins exist.
Plugins do not modify the core.
Open source the core and Plugin A. Keep Plugin B commercial.

### 5. Files are truth. Receipts prove work.
Every meaningful action produces a file artifact.
Nothing is done until it is written down and verifiable.
This applies to specs, plans, tasks, and implementation.

### 6. Human gates stay human
The system does not make decisions about what to do with data.
It captures, normalizes, and emits. A human (or the mesh)
decides what the data means and what action to take.

### 7. Legal capture scope is a hard boundary
In scope (legal, passive, no auth required):
- WiFi beacon frames: SSID, BSSID, encryption type, RSSI
- WiFi probe requests + ProbeSSIDs list
- BLE advertising packets: name, manufacturer ID, service UUIDs, RSSI
- mDNS on open networks

Never in scope (structural prohibition):
- Payload data of any kind
- Connecting to any network
- Deauth or active injection
- Authenticated traffic

### 8. Cross-platform means actually cross-platform
ARM6 (Pi Zero W), ARM7 (Pi 3B+), ARM64 (Pi 4B / Orange Pi),
x86_64 (dev/aggregator). One codebase, one build script,
four binaries. If it doesn't cross-compile clean, it's broken.

### 9. Open source as philosophy, not just license
MIT license. No bait-and-switch. No license change.
The community gets the core and the DeFlock plugin for real.
The commercial value is in the application layer, not in
locking down the infrastructure.

### 10. Rust Belt ethos
Gritty, not polished. Utility over elegance.
Ship something useful before shipping something pretty.
Scarcity is the context, not the obstacle.
This is a tool for people doing real work in the field.

## What success looks like
A canvasser puts a Pi Zero W in their bag, powers it on,
and walks a precinct. The device captures, normalizes,
and forwards RF data to an aggregator without requiring
any technical knowledge from the canvasser.
The mesh processes the data overnight.
By morning, the organizer has a picture of the surveillance
and network infrastructure along every route their team walked.

## What we will not build
- A web UI on the capture daemon
- A database on any leaf node
- Any active probing capability, even behind a flag
- A monolith that mixes capture + intelligence
- A proprietary core
- Anything that requires root access beyond monitor mode setup
