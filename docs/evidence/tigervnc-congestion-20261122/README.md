# TigerVNC congestion-window review (2026-11-22)

This is a source-level and arithmetic review of the Linux browser runtime's system TigerVNC dependency. It does not connect to the user's VPS, replace packages, run Chrome, or claim that the reported scroll lag was caused by this defect.

## Findings

- Debian 12 bookworm publishes `tigervnc-standalone-server_1.12.0+dfsg-8_amd64.deb`; the source file is available from [Debsources](https://sources.debian.org/src/tigervnc/1.12.0%2Bdfsg-8/common/rfb/Congestion.cxx/).
- Debian's 1.12.0 source and upstream `1.12-branch` retain two independent branches after a latency-spike reduction: the spike path sets `inSlowStart=false`, then the following congestion-avoidance branch can subtract 4096 from an already smaller unsigned window.
- Upstream commit [`895a86cff25b3d410af043098740d425cdad2bdc`](https://github.com/TigerVNC/tigervnc/commit/895a86cff25b3d410af043098740d425cdad2bdc), dated 2026-10-22, changes the second `if (inSlowStart)` to `else if`. The patch is one source line and is preserved here verbatim.
- Upstream stable tag `v1.16.2` still has the old branch; the current upstream `master` has the fixed branch. Therefore “upgrade to the latest stable TigerVNC” was not a sufficient fix at this review date.

## Reproduction

`reproduce.py` models only the exact unsigned arithmetic and clamp in `common/rfb/Congestion.cxx`; it is not a network simulation. With `baseRTT=100ms`, `minRTT=201ms`, and an initial 4KiB window, the latency difference is 101ms and satisfies the old spike condition. The old path scales the window to 2037 bytes, subtracts 4096 using 32-bit unsigned arithmetic, wraps, and clamps to the 4MiB maximum. The patched path skips the second branch and clamps the scaled value to the 4KiB minimum.

```sh
python3 reproduce.py
```

The saved `results.json` is the output of that command. `upstream-fix.patch` is fetched from the signed public Git history URL above. This evidence does not establish that the user's live connection generated the exact RTT sequence, that the wrap occurred in the running process, or that applying the patch alone improves browser scrolling.

## Product decision

The runtime currently invokes `/usr/bin/Xtigervnc` from the administrator's Debian package. Shikigami does not compile or replace the system VNC server. Applying this one-line fix would require a reviewed, signed runtime artifact or a distribution package that contains it, plus upgrade/rollback and live scrolling verification. Until those gates are available, no VPS package or installer behavior is changed. An isolated disposable Debian 12 container applied the patch and compiled TigerVNC's `rfb` common-library target successfully; the result is recorded in `build-result.txt`. It did not build or install `Xtigervnc`, and it did not provide a browser/frame-rate comparison. The lower-risk next experiment for a runtime claim remains a patched `Xtigervnc` artifact in an isolated environment; synthetic results must remain separate from live acceptance.
