#!/usr/bin/env python3
"""Reproduce the unsigned congestion-window wrap in TigerVNC pre-fix logic."""
import json
UINT32 = (1 << 32) - 1
MINIMUM_WINDOW = 4096
MAXIMUM_WINDOW = 4194304

def update(window, base_rtt, min_rtt, patched):
    diff = min_rtt - base_rtt
    in_slow_start = True
    if diff > max(100, base_rtt // 2):
        window = window * base_rtt // min_rtt
        in_slow_start = False
    if in_slow_start if patched else False:
        pass
    elif not patched:
        if diff > 50:
            window = (window - 4096) & UINT32
    if window < MINIMUM_WINDOW:
        window = MINIMUM_WINDOW
    if window > MAXIMUM_WINDOW:
        window = MAXIMUM_WINDOW
    return window

rows=[]
for initial in (4096, 8192, 16384):
    rows.append({
        "initial_window": initial,
        "base_rtt_ms": 100,
        "min_rtt_ms": 201,
        "latency_diff_ms": 101,
        "old_v1_12_window": update(initial, 100, 201, False),
        "patched_window": update(initial, 100, 201, True),
    })
print(json.dumps({"constants":{"minimum_window":MINIMUM_WINDOW,"maximum_window":MAXIMUM_WINDOW},"cases":rows}, indent=2))
