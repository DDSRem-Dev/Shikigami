#!/usr/bin/env python3
"""Summarize a completed forced-GC diagnostic; never infer leak/capacity acceptance."""

import argparse
import csv
import datetime
import hashlib
import json
from pathlib import Path

METRICS = ("rss", "heap_alloc", "heap_objects", "heap_inuse", "heap_idle",
           "heap_released", "go_sys", "goroutines", "fds")


def read(path):
    return json.loads(path.read_text())


def slope(times, values):
    mean_x, mean_y = sum(times) / len(times), sum(values) / len(values)
    denominator = sum((x - mean_x) ** 2 for x in times)
    return sum((x - mean_x) * (y - mean_y) for x, y in zip(times, values)) / denominator if denominator else None


def summarize(directory):
    directory = directory.resolve()
    experiment = read(directory / "windows.json")
    plan = experiment["plan"]
    profile_policy = plan.get("profile_policy")
    if profile_policy is not None and profile_policy not in ("each-boundary", "after-close"):
        raise ValueError("unknown diagnostic profile policy")
    version = experiment.get("schema_version", 1)
    mode = experiment.get("mode", "load")
    if version not in (1, 2) or mode not in ("load", "idle-control"):
        raise ValueError("unknown diagnostic schema/mode")
    if version == 2 and plan.get("mode") != mode:
        raise ValueError("diagnostic mode does not match plan")
    if version == 1 and mode != "load":
        raise ValueError("legacy diagnostics cannot claim idle-control")
    if not experiment["complete"] or not experiment["normal_build"]:
        raise ValueError("requires a completed ordinary-build diagnostic")
    windows, rounds = plan["windows"], plan["rounds_per_window"]
    measured_rounds = rounds if mode == "load" else 0
    expected_files = plan["warmup_rounds"] + windows * measured_rounds
    if (experiment["file_updates_total"] != expected_files
            or experiment["tcp_connections_total"] != 2 * expected_files
            or experiment["tcp_target_accepted"] != 2 * expected_files
            or len(experiment["windows"]) != windows):
        raise ValueError("incomplete fixed workload")
    if version == 2 and (experiment["measured_file_updates"] != windows*measured_rounds
                         or experiment["measured_tcp_connections"] != windows*measured_rounds*2):
        raise ValueError("mode-specific measured counters do not match")
    if version == 2:
        if mode == "idle-control" and experiment["current_file_sha256"] != experiment["warmup_file_sha256"]:
            raise ValueError("idle-control changed its warmed file")
        for path in (directory / "local-files/fixed.bin", directory / "projects/fixed/fixed.bin"):
            data = path.read_bytes()
            if len(data) != 256 << 10 or hashlib.sha256(data).hexdigest() != experiment["current_file_sha256"]:
                raise ValueError("archived data does not match the final verified file")
    if not plan["preflight"] and (windows != 5 or rounds != 16 or plan["load_duration_ns"] != 30_000_000_000):
        raise ValueError("unexpected full experiment plan")
    for window in experiment["windows"]:
        if window["file_updates"] != measured_rounds or window["tcp_connections"] != 2*measured_rounds:
            raise ValueError("window work does not match diagnostic mode")
        if (window["load_elapsed_ns"] < plan["load_duration_ns"]
                or window["idle_elapsed_ns"] < window["load_elapsed_ns"]):
            raise ValueError("shortened load or idle window")
    result = {"schema_version": version, "mode": mode, "experiment_sha256": hashlib.sha256((directory / "windows.json").read_bytes()).hexdigest(),
              "plan": plan, "roles": {}, "inference": "observations only; forced GC and sampling perturb the workload"}
    rows = []
    expected_profiles = set()
    for identity in experiment["processes"]:
        role = identity["role"]
        idle, loaded = [], []
        for index in range(windows + 1):
            for phase in (("idle",) if index == 0 else ("loaded", "idle")):
                label = f"window-{index:02d}-{phase}"
                point = read(directory / f"{role}.{label}.json")
                if point["PID"] != identity["pid"] or point["Role"] != role or point["Label"] != label:
                    raise ValueError("sample process or window identity mismatch")
                if profile_policy is not None and point.get("profile_policy") != profile_policy:
                    raise ValueError("sample profile policy does not match plan")
                if profile_policy == "each-boundary":
                    expected_profiles.update(f"{role}.{label}.{name}.pprof" for name in ("heap", "allocs", "goroutine"))
                entry = {"window": index, "at": point["At"], "after_gc": point["AfterGC"],
                         "states": point["States"], "history": point["History"]}
                (idle if phase == "idle" else loaded).append(entry)
                rows.append({"mode": mode, "role": role, "window": index, "phase": phase, "at": point["At"],
                             **{key: point["AfterGC"][key] for key in METRICS},
                             "states": json.dumps(point["States"], sort_keys=True),
                             "history": json.dumps(point["History"], sort_keys=True)})
        timestamps = [datetime.datetime.fromisoformat(p["at"].replace("Z", "+00:00")).timestamp() for p in idle]
        minutes = [(value - timestamps[0]) / 60 for value in timestamps]
        metrics = {}
        for name in METRICS:
            values = [p["after_gc"][name] for p in idle]
            metrics[name] = {"idle_values": values, "first_to_last": values[-1] - values[0],
                             "adjacent_deltas": [b - a for a, b in zip(values, values[1:])],
                             "ols_per_minute_all_idle": slope(minutes, values),
                             "ols_per_minute_last_three_idle": slope(minutes[-3:], values[-3:])}
        result["roles"][role] = {"identity": identity, "metrics": metrics, "idle": idle, "loaded": loaded}
        if profile_policy is not None and role in ("agent", "daemon"):
            closed = read(directory / f"{role}.closed.json")
            if (closed["PID"] != identity["pid"] or closed["Role"] != role or closed["Label"] != "closed"
                    or closed.get("profile_policy") != profile_policy):
                raise ValueError("closed sample identity/profile policy mismatch")
            expected_profiles.update(f"{role}.closed.{name}.pprof" for name in ("heap", "allocs", "goroutine"))
    if profile_policy is not None:
        if set(result["roles"]) != {"agent", "daemon", "sync-client", "sync-server", "port-client", "port-server"}:
            raise ValueError("profile policy verification requires all six roles")
        profiles = {path.name for path in directory.glob("*.pprof") if path.is_file() and path.stat().st_size > 0}
        if profiles != expected_profiles or len(list(directory.glob("*.pprof"))) != len(expected_profiles):
            raise ValueError("exported profile files do not match the selected policy")
        result["profile_exports"] = {"policy": profile_policy, "files": len(expected_profiles),
                                     "worker_closed_profiles": "unavailable: workers exit before host closed samples"}
    (directory / "window-summary.json").write_text(json.dumps(result, indent=2) + "\n")
    with (directory / "window-series.csv").open("w", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=("mode", "role", "window", "phase", "at", *METRICS, "states", "history"))
        writer.writeheader()
        writer.writerows(rows)
    print(json.dumps({"directory": str(directory), "mode": mode, "samples": len(rows), "preflight": plan["preflight"]}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="private runtime artifact directory containing windows.json")
    summarize(parser.parse_args().directory)
