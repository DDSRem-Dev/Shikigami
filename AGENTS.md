# Shikigami implementation rules

## Source of truth

The user requires the design documents in `docs/` to remain the implementation
baseline. Read `docs/README.md` and the relevant module documents before changing
behavior. The complete delivery scope is Phase 0–5 in `docs/08-roadmap.md`;
external environment gates remain pending until actually verified.

## Mandatory review after every batch

After each completed batch, review the entire implemented architecture, patterns,
and design against the documents before starting the next batch. This includes:

- Source directories, package dependency direction, SDK versus host ownership.
- RPC transports, service admission, version/capability claims and authorization.
- Persistent versus runtime state, atomicity, revisions, request deduplication.
- Process ownership, startup/shutdown, cancellation, crashes and recovery.
- File/terminal/network semantics, limits, defaults and error handling.
- CLI/TUI command contracts, configuration, installation and artifact layout.
- Behavior tests and documented acceptance evidence; skips are not passes.

Fix discovered discrepancies immediately and rerun the relevant checks. Do not
weaken or rewrite design requirements merely to legitimize existing code. A
planned feature that is not implemented must remain explicitly unsupported and
must not be advertised through capabilities, help, status or acceptance claims.
In the same batch, update `docs/08-roadmap.md` checkboxes and partial-progress
notes, `docs/15-implementation-status.md`, and findings/verification in
`docs/16-architecture-conformance.md`. A batch is not complete until these three
documents agree. Check an item only when every requirement in it has evidence;
otherwise leave it unchecked and record completed work and remaining gates.
Distinguish implementation/local acceptance from external/platform/release gates.

## Plugin boundary

Plugin entrypoints and business implementations belong under `plugins/sync/`
and `plugins/port/`. Host implementation belongs under `internal/plugin/`; public
plugin contracts and shared SDK support belong under `pkg/plugin/`. Plugin
production code must not import host `internal/` packages. Hosts must not import
plugin business implementations as an in-process fallback. Cross-process host
integration tests belong under `test/`, not inside plugin implementation packages.
`test/architecture` enforces the production dependency boundary.

## Collaboration

The user authorized subagents for the full implementation. Coordinate file
ownership and shared go.mod changes. After each assigned batch, report both
verification and document-conformance findings to the root agent. Preserve the
active local/Linux-container scope; do not assume VPS or external Tailscale
accounts are available.

## Long-running verification scheduling

While a long-running check is active, continue independent implementation,
review, documentation and acceptance work within the authorized local/container
scope. Do not turn the remaining roadmap into a polling-only queue. Freeze
source and isolate state/evidence for each concurrent check, coordinate shared
file ownership, and bound build/test concurrency to preserve existing runs.
Never restart an existing long run merely to include new changes; record its
actual source scope and verify new changes separately. Report completed work
and concrete dependencies, and synchronize partial progress without waiting
for a whole composite roadmap item to close.
