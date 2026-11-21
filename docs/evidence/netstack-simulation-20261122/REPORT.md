# Isolated gVisor TCP simulation — 2026-11-21 UTC

The experiment does **not** support promising a speed improvement from upgrading Tailscale/gVisor. Under this synthetic loss model, the new CUBIC configuration transfers more slowly despite fewer RTO events. This is neither an installed Shiki measurement nor proof of a production regression. No Shiki service, user network, account or node was changed.

## Reproducible model

Both binaries use Go 1.27.4 on Darwin arm64, GOMAXPROCS=2, build -p 1, and identical main.go. Old gVisor is v0.0.0-20260909230408-9b1144b679cb; new is v0.0.0-20260915211658-a6f909f08a72. Module and build-information files preserve full dependency pins.

Two independent stack.Stack instances communicate through official channel.Endpoint and gonet. IPv4, MTU 1280, SACK enabled, no GSO. Each direction serializes complete IP-packet bytes at 5,000,000 bit/s, followed by 55ms propagation. The pipeline does not serialize propagation per packet. Forward data-bearing TCP packets receive seeded Bernoulli loss of 0 or 1%; reverse ACK packets receive no injected loss. Drops still consume modeled serialization bandwidth. Queue capacity is 4096 and the primary observed maximum scheduled queue is 512. This is a model assumption, not a measured production queue.

Old primary configuration: Reno, explicit TCPRecovery(0). New primary configuration: CUBIC, default recovery setting (RACK enabled). The rack=true harness flag leaves the version's default untouched. Stack default send/receive buffers are used (1MiB default, 4MiB maximum), **not Tailscale's customized setTCPBufSizes**. This compares core gVisor configurations, not an exact tsnet embedding.

Each run transfers 2MiB of repeating binary payload, checks SHA256, and times from after a one-byte application ping to receiver io.CopyN completion, not final ACK. Application ping RTT is 112.69–114.35ms. Binary context deadline is 35s; runner timeout is 40s. Both primary versions are run in alternating order across seeds 901/902. Primary scheduler maximum delivery lateness is 4.057ms; one new/Reno/off control has 20.480ms lateness.

No real network, WireGuard, DERP, Tailscale server, HTTP/2, Chrome or viewer participates. No production loss rate has been measured; 1% is solely a test input.

## Results

All 11 recorded runs exit 0 and verify complete 2MiB payload SHA256.

| Matrix | Version | CC | RACK | Loss | Seed | Seconds | Mbps | Retransmits | RTO | Drops/matched |
|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| primary | old | reno | off | 0% | 901 | 3.743 | 4.482 | 0 | 0 | 0/0 |
| primary | new | cubic | default | 0% | 901 | 3.746 | 4.479 | 0 | 0 | 0/0 |
| primary | new | cubic | default | 0% | 902 | 3.745 | 4.480 | 0 | 0 | 0/0 |
| primary | old | reno | off | 0% | 902 | 3.745 | 4.480 | 0 | 0 | 0/0 |
| primary | old | reno | off | 1% | 901 | 5.287 | 3.174 | 35 | 2 | 16/15 |
| primary | new | cubic | default | 1% | 901 | 17.349 | 0.967 | 16 | 0 | 16/16 |
| primary | new | cubic | default | 1% | 902 | 24.650 | 0.681 | 29 | 0 | 29/29 |
| primary | old | reno | off | 1% | 902 | 6.418 | 2.614 | 42 | 2 | 29/28 |
| control | new | reno | off | 1% | 901 | 6.315 | 2.657 | 18 | 2 | 16/16 |
| control | new | cubic | off | 1% | 901 | 17.829 | 0.941 | 23 | 1 | 16/16 |
| control | new | reno | default | 1% | 901 | 5.582 | 3.005 | 25 | 2 | 16/16 |

The new-version seed901 controls show CUBIC remains slower with RACK disabled, while Reno with default RACK remains much faster in this model. This supports configuration/congestion-control influence for this scenario, not attribution to RACK alone. The controls have only one seed and one visibly jittery sample. No universal causal or deployment claim is warranted.

Retransmit/RTO/SACK counters come from the sender's official Stats().TCP counters. The custom recovery_delivery_ms metric measures from a modeled lost-packet arrival time to a later **single delivered segment completely covering that sequence range**. It is not internal TCP recovery duration. Each old-version loss run has one unmatched observation despite complete SHA success; segmentation or redundant-retransmission losses can escape this simple matcher. The raw field named unrecovered_drops therefore means unmatched observations, not application corruption or proof of unrecovered bytes. Repeated losses of the same sequence may also be collapsed by the matcher. Interpret only matched samples.

## Reproduction

Run these commands in this evidence directory. Compiled binaries were removed after collecting evidence and must be rebuilt. Source and module pins are preserved; reproduction timing is subject to host scheduling.

```sh
cp go.old.mod go.mod
cp go.old.sum go.sum
GOTOOLCHAIN=go1.27.4 GOMAXPROCS=2 go build -p 1 -o old .
cp go.new.mod go.mod
cp go.new.sum go.sum
GOTOOLCHAIN=go1.27.4 GOMAXPROCS=2 go build -p 1 -o new .
GOMAXPROCS=2 python3 run.py
GOMAXPROCS=2 python3 controls.py
```

The runners overwrite results.json and controls-results.json; copy evidence first if retaining the original measurements. This evidence is an isolated experiment and does not satisfy external/platform/release acceptance or prove persisted Tailscale identity upgrade/rollback compatibility. No main-repository code or roadmap claims were changed by this experiment.
