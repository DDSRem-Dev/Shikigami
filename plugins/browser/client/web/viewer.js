import RFB from "./novnc/core/rfb.js";

const byId = (id) => document.getElementById(id);
const protocol = "shiki-browser-rfb-v1";
const clipboardLimit = 64 * 1024;
const encoder = new TextEncoder();
const qualityModes = {
    smooth: {quality: 3, compression: 6, description: "Smooth: lower image quality and stronger compression to reduce traffic."},
    balanced: {quality: 6, compression: 2, description: "Balanced: the original image quality and compression settings."},
    clear: {quality: 8, compression: 2, description: "Clear: higher image quality, which may require more bandwidth."},
};
const state = {
    session: null, csrf: "", rfb: null, channel: null, generation: 0,
    screenReady: false, attached: false, controller: false, canControl: false,
    attachment: "", instance: "", revision: "", controllerId: "",
    pendingControl: false, connecting: false, closed: false, transferring: false,
    statusRequest: null, controlRequest: null, statusTimer: null, screenClosing: null,
    qualityMode: "balanced", transportTimer: null,
};

function applyQuality() {
    const mode = qualityModes[state.qualityMode];
    byId("quality-mode").value = state.qualityMode;
    byId("quality-mode").title = mode.description;
    byId("quality-description").textContent = mode.description + " Applies to this viewer; remote resolution and control permissions are unchanged.";
    if (state.rfb) {
        state.rfb.qualityLevel = mode.quality;
        state.rfb.compressionLevel = mode.compression;
    }
}

function updateTransport(channel) {
    if (state.channel !== channel || state.closed) return;
    const now = performance.now();
    const elapsed = now - channel.sampleTime;
    if (elapsed <= 0) return;
    const rate = (channel.receivedBytes - channel.sampleBytes) * 1000 / elapsed / 1024;
    byId("transport-status").textContent = `Received screen data: ${rate.toFixed(1)} KiB/s · Local send buffer: ${(channel.socket.bufferedAmount / 1024).toFixed(1)} KiB`;
    channel.sampleBytes = channel.receivedBytes;
    channel.sampleTime = now;
}

function showMessage(message, info = false) {
    const element = byId("message");
    element.textContent = message;
    element.className = info ? "info" : "";
    element.hidden = !message;
}

async function api(endpoint, {method = "GET", json, body, binary = false, signal} = {}) {
    const headers = {"X-Shiki-CSRF": state.csrf};
    if (json !== undefined) headers["Content-Type"] = "application/json";
    if (body !== undefined) headers["Content-Type"] = "application/octet-stream";
    let response;
    try {
        response = await fetch(`api/${endpoint}`, {
            method, headers, signal, credentials: "same-origin", mode: "same-origin",
            cache: "no-store", redirect: "error",
            body: json === undefined ? body : JSON.stringify(json),
        });
    } catch {
        throw new Error("The local viewer request failed. Its outcome is unknown; refresh status before repeating a change.");
    }
    if (!response.ok) {
        let failure;
        try { failure = await response.json(); } catch { /* Non-JSON admission failure. */ }
        throw new Error(failure?.message || `The viewer rejected the request (HTTP ${response.status}). The session may have expired; open a new viewer from shiki.`);
    }
    return binary ? response : response.json();
}

function renderAuthority() {
    const controlling = state.controller && state.screenReady && !state.closed;
    if (state.rfb) {
        // Disable remote resize before relinquishing input. Local scaling never
        // changes the server framebuffer size.
        state.rfb.resizeSession = controlling && byId("resize-session").checked;
        state.rfb.viewOnly = !controlling;
    }
    if (!controlling) byId("resize-session").checked = false;
    byId("resize-session").disabled = !controlling;
    const role = byId("control-status");
    role.textContent = controlling ? "Controller" : state.session?.readOnly || !state.canControl ? "Read-only session" : "Read-only";
    role.classList.toggle("controller", controlling);
    role.title = state.revision ? `Control revision: ${state.revision}` : "Control state is not known yet";
    byId("take-control").disabled = !state.screenReady || !state.canControl || controlling || state.pendingControl || state.closed;
    byId("take-control").textContent = state.pendingControl ? "Waiting for control update…" : "Take control";
    byId("paste-clipboard").disabled = !controlling;
    byId("reconnect").disabled = !state.session || state.connecting || state.closed;
    byId("connection-status").textContent = state.connecting ? "Connecting" : state.screenReady ? "Connected" : "Disconnected";
    byId("refresh-status").disabled = state.closed;
    byId("close-viewer").disabled = state.closed;
    byId("quality-mode").disabled = state.closed;
    byId("read-clipboard").disabled = state.closed;
    byId("copy-clipboard").disabled = state.closed || !byId("remote-clipboard").value;
    byId("local-clipboard").disabled = state.closed;
    setTransferring(state.transferring);
}

function detachScreen() {
    // Invalidate callbacks before closing the old channel. A delayed HTTP
    // takeover/status response must not affect a replacement attachment.
    ++state.generation;
    clearInterval(state.transportTimer);
    state.transportTimer = null;
    byId("transport-status").textContent = "Screen transport inactive.";
    const statusRequest = state.statusRequest, controlRequest = state.controlRequest;
    state.statusRequest = state.controlRequest = null;
    statusRequest?.abort.abort();
    controlRequest?.abort.abort();
    const rfb = state.rfb;
    const channel = state.channel;
    state.rfb = null;
    state.channel = null;
    state.controller = false;
    state.screenReady = false;
    state.attached = false;
    state.connecting = false;
    state.pendingControl = false;
    if (rfb) rfb.disconnect();
    if (channel) {
        const closing = channel.closed;
        state.screenClosing = closing;
        closing.then(() => { if (state.screenClosing === closing) state.screenClosing = null; });
        channel.close();
    }
    renderAuthority();
    return state.screenClosing;
}

// noVNC supports a raw WebSocket-like channel. This adapter delivers only binary
// RFB bytes to noVNC; typed viewer metadata stays outside the framebuffer parser.
// The only network endpoint is this session's authenticated loopback path.
class ViewerChannel {
    constructor(url, csrf, onEvent) {
        this.onopen = null;
        this.onmessage = null;
        this.onclose = null;
        this.onerror = null;
        this.binaryType = "arraybuffer";
        this.protocol = "";
        this.revision = "";
        this.readyState = WebSocket.CONNECTING;
        this.onEvent = onEvent;
        this.receivedBytes = this.sampleBytes = 0;
        this.sampleTime = performance.now();
        this.closed = new Promise((resolve) => { this.resolveClosed = resolve; });
        this.socket = new WebSocket(url, [protocol, `csrf.${csrf}`]);
        this.socket.binaryType = "arraybuffer";
        this.socket.onopen = (event) => {
            this.protocol = this.socket.protocol;
            this.readyState = WebSocket.OPEN;
            if (this.protocol !== protocol) { this.fail("Viewer WebSocket subprotocol validation failed."); return; }
            this.onopen?.(event);
        };
        this.socket.onmessage = (event) => {
            if (typeof event.data === "string") {
                try {
                    const metadata = JSON.parse(event.data);
                    if (!metadata || typeof metadata.type !== "string") throw new Error("Invalid metadata");
                    if (metadata.type === "ready" || metadata.type === "control") {
                        if (typeof metadata.revision !== "string" || encoder.encode(metadata.revision).byteLength > 256) throw new Error("Invalid control revision");
                        this.revision = metadata.revision;
                    }
                    this.onEvent(metadata);
                } catch { this.fail("The viewer received invalid screen metadata. Remote state is unknown; reconnect explicitly."); }
                return;
            }
            if (!(event.data instanceof ArrayBuffer) || event.data.byteLength > 1024 * 1024) {
                this.fail("The remote screen exceeded the viewer's bounded frame limit.");
                return;
            }
            this.receivedBytes += event.data.byteLength;
            this.onmessage?.(event);
        };
        this.socket.onerror = (event) => {
            this.onEvent({type: "error", message: "The screen connection failed for an unknown reason. Refresh status to check the remote browser; no input will be replayed."});
            this.onerror?.(event);
        };
        this.socket.onclose = (event) => {
            this.readyState = WebSocket.CLOSED;
            this.resolveClosed();
            this.onclose?.(event);
        };
    }
    send(data) {
        if (this.socket.readyState !== WebSocket.OPEN) throw new Error("The viewer screen is disconnected");
        const revision = encoder.encode(this.revision);
        const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
        const length = 2 + revision.byteLength + bytes.byteLength;
        if (this.socket.bufferedAmount + length > 1024 * 1024) {
            this.fail("Input backpressure exceeded the 1 MiB queue limit. The screen was detached, not retried; reconnect explicitly.");
            throw new Error("Viewer input backpressure limit exceeded");
        }
        // Capture the revision before WebSocket buffering, so input queued
        // before a takeover can never acquire a newer control lease.
        const envelope = new Uint8Array(length);
        envelope[0] = revision.byteLength >> 8;
        envelope[1] = revision.byteLength & 255;
        envelope.set(revision, 2);
        envelope.set(bytes, 2 + revision.byteLength);
        this.socket.send(envelope);
    }
    close() {
        if (this.socket.readyState < WebSocket.CLOSING) {
            this.readyState = WebSocket.CLOSING;
            this.socket.close(1000, "Viewer detached");
        }
    }
    fail(message) {
        this.onEvent({type: "error", message});
        this.readyState = WebSocket.CLOSING;
        this.socket.close(1008, "Viewer protocol or queue limit");
    }
}

function screenEvent(event, generation) {
    if (generation !== state.generation || state.closed) return;
    if (event.type === "ready" || event.type === "control") {
        if (typeof event.attachment !== "string" || typeof event.instance !== "string" || typeof event.revision !== "string") throw new Error("Incomplete control metadata");
        if (event.type === "control" && (!state.attached || event.attachment !== state.attachment || event.instance !== state.instance)) throw new Error("Control metadata belongs to another attachment");
        state.attached = true;
        state.attachment = event.attachment;
        state.instance = event.instance;
        state.revision = event.revision;
        state.controllerId = event.controllerId || "";
        if (event.type === "ready") state.canControl = !state.session.readOnly && event.canControl === true;
        state.controller = state.canControl && event.controller === true && event.readOnly === false;
        state.pendingControl = false;
        renderAuthority();
        const request = state.controlRequest;
        if (currentControlRequest(request)) {
            if (state.controller) request.confirmed = true;
            if (request.confirmed) showMessage(controlResultMessage(request), true);
        }
    } else if (event.type === "closed" || event.type === "error") {
        detachScreen();
        showMessage(event.message || "Unknown screen failure. Refresh status; the independent browser may still be running.");
    } else {
        throw new Error("Unknown viewer metadata type");
    }
}

async function connectScreen() {
    if (!state.session || state.closed || state.connecting) return;
    // This is an explicit action, including when an open screen has stalled.
    // The replacement starts read-only and never restores or replays input.
    const previousClosed = detachScreen();
    const generation = state.generation;
    state.connecting = true;
    state.canControl = false;
    state.attachment = state.instance = state.revision = state.controllerId = "";
    byId("screen").replaceChildren();
    showMessage("");
    renderAuthority();
    try {
        if (previousClosed) {
            // Let the old attachment close before creating its replacement.
            // Otherwise a new socket can race VIEWER_ALREADY_ATTACHED.
            let timer;
            try {
                await Promise.race([previousClosed, new Promise((_, reject) => {
                    timer = window.setTimeout(() => reject(new Error("Previous screen did not close")), 5000);
                })]);
            } finally { clearTimeout(timer); }
            if (generation !== state.generation || state.closed) return;
        }
        const url = new URL("rfb", window.location.href);
        url.protocol = "ws:";
        const channel = new ViewerChannel(url.href, state.csrf, (event) => screenEvent(event, generation));
        state.channel = channel;
        const rfb = new RFB(byId("screen"), channel, {shared: true});
        state.rfb = rfb;
        applyQuality();
        state.transportTimer = window.setInterval(() => updateTransport(channel), 1000);
        rfb.viewOnly = true;
        rfb.scaleViewport = true;
        rfb.resizeSession = false;
        rfb.focusOnClick = true;
        rfb.addEventListener("connect", () => {
            if (generation !== state.generation) return;
            state.connecting = false;
            state.screenReady = true;
            renderAuthority();
            showMessage("Connected read-only. Use Take control before sending keyboard, mouse, clipboard, or remote resize requests.", true);
            refreshStatus();
        });
        rfb.addEventListener("disconnect", (event) => {
            if (generation !== state.generation) return;
            // noVNC can emit disconnect before its WebSocket has closed.
            // Preserve that close barrier and invalidate outstanding callbacks
            // before allowing an explicit replacement attachment.
            detachScreen();
            if (!state.closed && !event.detail.clean) showMessage("The screen disconnected unexpectedly; remote browser state is unknown. Refresh status, then reconnect explicitly. Input is never replayed.");
        });
        rfb.addEventListener("securityfailure", () => {
            if (generation === state.generation) showMessage("Remote RFB security negotiation failed. This viewer does not accept VNC credentials or fall back to an arbitrary VNC endpoint.");
        });
        rfb.addEventListener("credentialsrequired", () => {
            if (generation !== state.generation) return;
            showMessage("The managed remote screen unexpectedly requested credentials. The attachment was closed; inspect the server runtime configuration.");
            detachScreen();
        });
        rfb.addEventListener("clipboard", (event) => {
            if (generation !== state.generation || !state.controller) return;
            const text = event.detail.text;
            if (encoder.encode(text).byteLength > clipboardLimit) {
                byId("remote-clipboard").value = "";
                byId("copy-clipboard").disabled = true;
                showMessage("The remote clipboard exceeds the 64 KiB UI limit and was not copied.");
                return;
            }
            byId("remote-clipboard").value = text;
            byId("copy-clipboard").disabled = !text;
        });
    } catch {
        if (generation !== state.generation || state.closed) return;
        detachScreen();
        showMessage("The screen could not reconnect or initialize. Remote Chrome may still be running; refresh status and use Reconnect screen again. Input is never replayed.");
    }
}

function statusPreview(value, limit) {
    const text = String(value).replace(/\s+/g, " ").trim();
    return text.length > limit ? text.slice(0, limit) + "… (see Details)" : text;
}

function renderRemoteStatus(status) {
    const remoteState = status.state || "unknown";
    const mode = status.mode === "browse" ? "Browsing" : status.mode === "diagnostic" ? "Diagnostics" : "unknown";
    const chrome = status.chrome_version || "unknown";
    const warnings = Array.isArray(status.warnings) ? status.warnings : [];
    const summary = [`State: ${statusPreview(remoteState, 40)}`, `Mode: ${mode}`, `Attachments: ${status.attachments || 0}`, `Chrome: ${statusPreview(chrome, 48)}`];
    const details = [`State: ${remoteState}`, `Mode: ${mode}`, `Attachments: ${status.attachments || 0}`, `Chrome: ${chrome}`];
    if (status.restart_required) {
        summary.push("Restart required (not automatic)");
        details.push("Configuration changed: remote restart required (not automatic)");
    }
    if (status.reason) {
        summary.push(`Reason: ${statusPreview(status.reason, 160)}`);
        details.push(`Reason: ${status.reason}`);
    }
    summary.push(`Warnings: ${warnings.length}${warnings.length ? " (see Details)" : ""}`);
    details.push(`Warnings: ${warnings.length}`, ...warnings.map((warning, index) => `${index + 1}. ${warning}`));
    byId("remote-status").textContent = summary.join(" · ");
    byId("status-details-label").textContent = "Remote status Details";
    byId("status-details-content").textContent = details.join("\n");
}

async function refreshStatus() {
    if (!state.session || state.closed || state.statusRequest) return;
    const generation = state.generation;
    const request = {generation, abort: new AbortController()};
    state.statusRequest = request;
    try {
        const status = await api("status", {signal: request.abort.signal});
        if (state.closed || generation !== state.generation || state.statusRequest !== request) return;
        renderRemoteStatus(status);
        // Stream control updates, not a potentially stale HTTP snapshot, govern
        // input. A status request must never silently grant this page control.
        if (!state.attached) state.revision = status.control_revision || "";
        if (state.instance && status.instance_id && state.instance !== status.instance_id) {
            detachScreen();
            showMessage("The managed browser instance changed. Reconnect the screen explicitly; no input will be replayed.");
        }
    } catch (error) {
        if (!state.closed && generation === state.generation && state.statusRequest === request) {
            byId("remote-status").textContent = `Remote browser status: unknown. ${error.message}`;
            byId("status-details-label").textContent = "Previous status Details — latest refresh failed";
        }
    } finally { if (state.statusRequest === request) state.statusRequest = null; }
}

function currentControlRequest(request) {
    return request && state.controlRequest === request && !state.closed && request.generation === state.generation && request.attachment === state.attachment && request.instance === state.instance;
}

function controlResultMessage(request) {
    if (request.confirmed) return state.controller
        ? "You control this remote browser. Click inside the screen to use the mouse and keyboard."
        : "The screen confirmed the takeover; this viewer is now read-only.";
    return "Takeover accepted; waiting for the screen to confirm control. If the screen stays unresponsive, use Reconnect screen and take control again.";
}

byId("refresh-status").addEventListener("click", refreshStatus);
byId("quality-mode").addEventListener("change", () => {
    if (state.closed) return;
    const selected = byId("quality-mode").value;
    if (Object.hasOwn(qualityModes, selected)) state.qualityMode = selected;
    applyQuality();
});
byId("reconnect").addEventListener("click", connectScreen);
byId("resize-session").addEventListener("change", renderAuthority);
byId("take-control").addEventListener("click", async () => {
    if (!state.screenReady || !state.canControl || state.controller || state.pendingControl) return;
    const expected = state.revision;
    const generation = state.generation;
    const attachment = state.attachment, instance = state.instance;
    const description = state.controllerId ? "The current controller will become read-only." : "No viewer will be promoted automatically.";
    if (!window.confirm(`Take control of this remote browser? ${description}\nExpected control revision: ${expected}`)) return;
    state.controlRequest?.abort.abort();
    const request = {generation, attachment, instance, confirmed: false, abort: new AbortController()};
    state.controlRequest = request;
    state.pendingControl = true;
    renderAuthority();
    try {
        await api("control", {method: "POST", json: {revision: expected, attachment, instance}, signal: request.abort.signal});
        if (!currentControlRequest(request)) return;
        showMessage(controlResultMessage(request), true);
        refreshStatus();
    } catch (error) {
        if (!currentControlRequest(request)) return;
        state.pendingControl = false;
        renderAuthority();
        showMessage(request.confirmed ? controlResultMessage(request) : error.message, request.confirmed);
        refreshStatus();
    }
});

byId("read-clipboard").addEventListener("click", async () => {
    try {
        if (!navigator.clipboard?.readText) throw new Error("Use your normal paste shortcut in the text box; this browser does not expose clipboard read access.");
        const text = await navigator.clipboard.readText();
        if (encoder.encode(text).byteLength > clipboardLimit) throw new Error("The local clipboard exceeds 64 KiB and was not loaded.");
        byId("local-clipboard").value = text;
        showMessage("Local clipboard loaded into the box only. Click Send to transfer it remotely.", true);
    } catch (error) { showMessage(`Local clipboard read unavailable. ${error.message}`); }
});
byId("copy-clipboard").addEventListener("click", async () => {
    try {
        if (!navigator.clipboard?.writeText) throw new Error("Select the remote text box and use your normal copy shortcut instead.");
        await navigator.clipboard.writeText(byId("remote-clipboard").value);
        showMessage("Remote text copied to the local clipboard by your explicit request.", true);
    } catch (error) { showMessage(`Local clipboard copy unavailable. ${error.message}`); }
});
byId("paste-clipboard").addEventListener("click", () => {
    if (!state.controller || !state.screenReady || !state.rfb) return;
    const text = byId("local-clipboard").value;
    if (encoder.encode(text).byteLength > clipboardLimit) { showMessage("Clipboard text exceeds 64 KiB and was not sent."); return; }
    state.rfb.clipboardPasteFrom(text);
    showMessage("Text sent to the remote clipboard. Paste inside remote Chrome using its normal shortcut.", true);
    state.rfb.focus();
});

function setTransferring(value) {
    state.transferring = value;
    byId("upload").disabled = value || state.closed;
    byId("refresh-files").disabled = value || state.closed;
    byId("upload-file").disabled = byId("upload-name").disabled = byId("file-area").disabled = value || state.closed;
    for (const button of byId("file-list").querySelectorAll("button")) button.disabled = value || state.closed || button.dataset.overLimit === "true";
}

async function listFiles() {
    if (!state.session || state.closed || state.transferring) return;
    byId("file-list").replaceChildren();
    byId("file-status").textContent = "Loading staging metadata…";
    try {
        const area = byId("file-area").value;
        const listing = await api(`files?${new URLSearchParams({area})}`);
        const files = listing.files || [];
        for (const file of files) {
            const size = Number(file.size || 0);
            const row = document.createElement("tr");
            for (const [value, className] of [[file.name, ""], [String(size), ""], [file.sha256 || "unknown", "checksum"]]) {
                const cell = document.createElement("td");
                cell.textContent = value;
                cell.className = className;
                row.append(cell);
            }
            const action = document.createElement("td");
            const button = document.createElement("button");
            const valid = Number.isSafeInteger(size) && size >= 0 && size <= state.session.maxFileBytes && /^[a-f0-9]{64}$/.test(file.sha256 || "");
            button.textContent = "Download";
            button.disabled = !valid;
            button.dataset.overLimit = String(!valid);
            if (!valid) button.title = "The file exceeds this viewer's byte limit or has unknown integrity metadata";
            button.addEventListener("click", () => downloadFile({...file, size, area}));
            action.append(button);
            row.append(action);
            byId("file-list").append(row);
        }
        byId("file-status").textContent = `${files.length} ${area} file(s). This is a metadata snapshot; changed files are rejected on download.`;
    } catch (error) { byId("file-status").textContent = `File list unavailable. ${error.message}`; }
}

async function downloadFile(file) {
    if (state.transferring || state.closed) return;
    setTransferring(true);
    byId("file-status").textContent = `Validating and retrieving ${file.name}…`;
    try {
        const response = await api("files/download", {method: "POST", json: {area: file.area, name: file.name, size: file.size, sha256: file.sha256}, binary: true});
        if (response.headers.get("X-Shiki-SHA256") !== file.sha256) throw new Error("The verified download checksum header was missing or changed. Nothing was saved.");
        const blob = await response.blob();
        if (blob.size !== file.size) throw new Error("The local HTTP transfer was truncated. Nothing was saved.");
        const href = URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = href;
        link.download = file.name;
        document.body.append(link);
        link.click();
        link.remove();
        window.setTimeout(() => URL.revokeObjectURL(href), 60000);
        byId("file-status").textContent = `Validated ${file.size} bytes (SHA-256 ${file.sha256}). A local download was requested; check your browser's download result.`;
    } catch (error) { byId("file-status").textContent = `Download failed. ${error.message}`; }
    finally { setTransferring(false); }
}

byId("refresh-files").addEventListener("click", listFiles);
byId("file-area").addEventListener("change", () => {
    byId("file-list").replaceChildren();
    byId("file-status").textContent = "Area changed. Click List staging files to request metadata.";
});
byId("upload-file").addEventListener("change", () => {
    byId("upload-name").value = byId("upload-file").files[0]?.name || "";
});
byId("upload").addEventListener("click", async () => {
    if (!state.session || state.closed || state.transferring) return;
    const file = byId("upload-file").files[0];
    if (!file) { byId("file-status").textContent = "Choose a local file first; choosing alone does not upload it."; return; }
    if (file.size > state.session.maxFileBytes) { byId("file-status").textContent = `This file exceeds the configured ${state.session.maxFileBytes}-byte limit and was not uploaded.`; return; }
    const name = byId("upload-name").value;
    if (!name || name === "." || name === ".." || name.startsWith(".") || name.trim() !== name || encoder.encode(name).byteLength > 240 || /[\/\\\u0000-\u001f\u007f]/.test(name)) { byId("file-status").textContent = "Choose a flat staging filename of at most 240 UTF-8 bytes, without a leading dot, surrounding whitespace, path separators, or control characters."; return; }
    if (!window.confirm(`Upload ${file.name} as ${name} (${file.size} bytes) to this profile's uploads staging area?`)) return;
    setTransferring(true);
    byId("file-status").textContent = `Calculating SHA-256 for ${file.name}…`;
    try {
        const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
        const sha256 = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
        byId("file-status").textContent = `Uploading ${name}; no automatic retries…`;
        const result = await api(`files/upload?${new URLSearchParams({name, size: String(file.size), sha256})}`, {method: "PUT", body: file});
        byId("file-status").textContent = `Uploaded ${result.name}: ${file.size} bytes, SHA-256 ${sha256}. Select Uploads and list files to inspect staging.`;
    } catch (error) { byId("file-status").textContent = `Upload failed or its outcome is unknown. ${error.message} Refresh uploads before any retry.`; }
    finally { setTransferring(false); }
});

byId("close-viewer").addEventListener("click", async () => {
    if (state.closed) return;
    try {
        await api("close", {method: "POST"});
        showMessage("Viewer session closed and its local credential revoked. The independent remote Chrome was not stopped.", true);
    } catch (error) {
        showMessage(`The viewer could not confirm credential revocation. The screen is detached locally; close this viewer from shiki or wait for its expiry. ${error.message}`);
    }
    state.closed = true;
    clearInterval(state.statusTimer);
    detachScreen();
    byId("remote-clipboard").value = byId("local-clipboard").value = "";
    byId("copy-clipboard").disabled = byId("read-clipboard").disabled = byId("close-viewer").disabled = true;
    setTransferring(false);
});

window.addEventListener("pagehide", detachScreen);
window.addEventListener("error", () => {
    if (!state.closed) {
        detachScreen();
        showMessage("The viewer encountered an unexpected local error and detached the screen. Remote browser state is unknown. Refresh status or reconnect explicitly.");
    }
});
window.addEventListener("unhandledrejection", () => {
    if (!state.closed) {
        detachScreen();
        showMessage("The viewer encountered an unexpected asynchronous error and detached the screen. Remote browser state is unknown; no input will be replayed.");
    }
});

(async () => {
    try {
        const response = await fetch("api/session", {credentials: "same-origin", mode: "same-origin", cache: "no-store", redirect: "error"});
        if (!response.ok) throw new Error("Viewer session expired or invalid. Open a new viewer from shiki.");
        const session = await response.json();
        if (!/^[A-Za-z0-9_-]{43}$/.test(session.csrf) || !Number.isSafeInteger(session.maxFileBytes) || session.maxFileBytes < 1) throw new Error("Invalid local viewer session response");
        state.session = session;
        state.csrf = session.csrf;
        applyQuality();
        byId("scope").textContent = `Server: ${session.server} · Profile: ${session.profile}`;
        byId("file-limit").textContent = `Configured file limit: ${session.maxFileBytes} bytes per file; one active transfer per viewer. Downloads are temporarily spooled in the private client plugin state and deleted when the request finishes.`;
        // Status is useful diagnostics, not a prerequisite for attaching.
        // Start both so one slow status request cannot delay the screen.
        connectScreen();
        refreshStatus();
        state.statusTimer = window.setInterval(() => { if (!document.hidden) refreshStatus(); }, 10000);
        window.setTimeout(() => {
            state.closed = true;
            clearInterval(state.statusTimer);
            byId("remote-clipboard").value = byId("local-clipboard").value = "";
            detachScreen();
            setTransferring(false);
            showMessage("This ephemeral viewer session expired. Open a new viewer from shiki; remote Chrome was not stopped.");
        }, Math.max(0, session.expiresAt * 1000 - Date.now()));
    } catch (error) {
        state.closed = true;
        renderAuthority();
        showMessage(error.message || "Unknown viewer initialization failure. Open a new viewer from shiki.");
    }
})();
