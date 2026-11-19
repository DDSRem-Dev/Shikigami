import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {test} from 'node:test';
import vm from 'node:vm';

// Execute the shipped viewer script. The harness replaces browser/noVNC I/O,
// not the viewer's authority or lifecycle logic. No network or user state.
const source = (await readFile(new URL('./web/viewer.js', import.meta.url), 'utf8'))
    .replace('import RFB from "./novnc/core/rfb.js";', 'const RFB = globalThis.TestRFB;');
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return {promise, resolve}; };
const response = (data, status = 200) => ({ok: status === 200, status, json: async () => data});

async function viewer({holdStatus = false, holdClose = false} = {}) {
    const elements = new Map(), rfbs = [], sockets = [], requests = [], controls = [], statuses = [];
    const element = id => {
        if (!elements.has(id)) elements.set(id, {
            textContent: '', value: '', checked: false, disabled: false, hidden: false,
            listeners: {}, classList: {toggle() {}}, querySelectorAll: () => [], replaceChildren() {},
            addEventListener(type, callback) { this.listeners[type] = callback; },
        });
        return elements.get(id);
    };
    class Socket {
        static CONNECTING = 0; static OPEN = 1; static CLOSING = 2; static CLOSED = 3;
        readyState = 0; bufferedAmount = 0;
        constructor() { sockets.push(this); }
        close() { this.readyState = 3; if (!holdClose) queueMicrotask(() => this.onclose?.({})); }
    }
    class RFB {
        listeners = {}; viewOnly = true;
        constructor(_target, channel) { this.channel = channel; rfbs.push(this); }
        addEventListener(type, callback) { this.listeners[type] = callback; }
        disconnect() { this.disconnected = true; this.listeners.disconnect?.({detail: {clean: true}}); }
    }
    const fetch = async (url, options = {}) => {
        requests.push({url, method: options.method || 'GET', body: options.body, signal: options.signal});
        if (url === 'api/session') return response({csrf: 'a'.repeat(43), maxFileBytes: 1048576, expiresAt: Date.now()/1000 + 3600, readOnly: false});
        if (url === 'api/control') { const d = deferred(); controls.push(d); return d.promise; }
        if (url === 'api/status') {
            if (holdStatus) { const d = deferred(); statuses.push(d); return d.promise; }
            return response({state: 'running', instance_id: 'instance'});
        }
        throw new Error(`Unexpected request ${url}`);
    };
    const context = vm.createContext({
        TestRFB: RFB, WebSocket: Socket, TextEncoder, Uint8Array, ArrayBuffer, URL, URLSearchParams, AbortController,
        fetch, clearInterval() {}, clearTimeout() {}, document: {getElementById: element, hidden: false},
        window: {location: {href: 'http://127.0.0.1:1234/v/session/'}, confirm: () => true,
            addEventListener() {}, setInterval() {}, setTimeout() {}},
    });
    vm.runInContext(source, context);
    await flush();
    const connect = (index = rfbs.length - 1, instance = 'instance') => {
        const rfb = rfbs[index];
        rfb.channel.onEvent({type: 'ready', attachment: `attachment-${index}`, instance, revision: `revision-${index}`, canControl: true, readOnly: true, controller: false});
        rfb.listeners.connect();
    };
    const click = async id => { const pending = element(id).listeners.click(); await flush(); return {pending}; };
    return {element, rfbs, sockets, requests, controls, statuses, connect, click};
}

test('slow status does not block screen attachment; status failure does not revoke stream control', async () => {
    const v = await viewer({holdStatus: true});
    assert.equal(v.rfbs.length, 1);
    v.connect();
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'revision-1', controller: true, readOnly: false});
    assert.equal(v.rfbs[0].viewOnly, false);
    v.statuses[0].resolve(response({message: 'Status temporarily unavailable'}, 503));
    await flush();
    assert.equal(v.rfbs[0].viewOnly, false);
    assert.match(v.element('remote-status').textContent, /unknown/);
    assert.equal(v.rfbs.length, 1, 'HTTP status failure must not reconnect or replay input');
});

test('explicit reconnect replaces even an open screen and returns to read-only', async () => {
    const v = await viewer(); v.connect(); await flush();
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'revision-1', controller: true, readOnly: false});
    assert.equal(v.element('reconnect').disabled, false);
    await v.click('reconnect');
    assert.equal(v.rfbs[0].disconnected, true);
    assert.equal(v.rfbs.length, 2);
    v.connect();
    assert.equal(v.rfbs[1].viewOnly, true);
    assert.equal(v.requests.filter(r => r.method !== 'GET').length, 0);
    v.rfbs[0].listeners.disconnect({detail: {clean: false}});
    assert.equal(v.element('connection-status').textContent, 'Connected', 'old disconnect cannot detach replacement');
});

test('remote instance change detaches stale screen and permits explicit reconnect', async () => {
    const v = await viewer({holdStatus: true}); v.connect();
    v.statuses[0].resolve(response({state: 'running', instance_id: 'new-instance'}));
    await flush();
    assert.equal(v.rfbs[0].disconnected, true);
    assert.equal(v.element('reconnect').disabled, false);
    assert.equal(v.element('connection-status').textContent, 'Disconnected');
    assert.equal(v.rfbs.length, 1);
});

for (const success of [true, false]) test(`stale takeover ${success ? 'success' : 'failure'} cannot affect a replacement attachment`, async () => {
    const v = await viewer(); v.connect(); await flush();
    const first = await v.click('take-control');
    assert.equal(v.controls.length, 1);
    v.rfbs[0].listeners.disconnect({detail: {clean: false}});
    await v.click('reconnect'); v.connect(); await flush();
    const second = await v.click('take-control');
    assert.equal(v.controls.length, 2);
    const message = v.element('message').textContent;
    v.controls[0].resolve(response({message: 'Old takeover failed'}, success ? 200 : 503));
    await first.pending; await flush();
    assert.equal(v.element('take-control').textContent, 'Waiting for control update…');
    assert.equal(v.element('message').textContent, message);
    assert.equal(v.rfbs[1].viewOnly, true);
    v.controls[1].resolve(response({})); await second.pending;
    assert.equal(v.rfbs[1].viewOnly, true, 'HTTP success alone must not grant input');
    v.rfbs[1].channel.onEvent({type: 'control', attachment: 'attachment-1', instance: 'instance', revision: 'revision-3', controller: true, readOnly: false});
    assert.equal(v.rfbs[1].viewOnly, false);
    assert.match(v.element('message').textContent, /You control/);
    assert.equal(v.controls.length, 2, 'only explicit takeovers are submitted');
});

test('terminal stream error detaches immediately without automatic replay', async () => {
    const v = await viewer(); v.connect();
    v.rfbs[0].channel.onEvent({type: 'error', message: 'Transport unavailable'});
    assert.equal(v.element('connection-status').textContent, 'Disconnected');
    assert.equal(v.element('reconnect').disabled, false);
    assert.equal(v.rfbs[0].disconnected, true);
    assert.equal(v.rfbs.length, 1);
});


test('replacement waits for old websocket closure before opening another attachment', async () => {
    const v = await viewer({holdClose: true}); v.connect(); await flush();
    const reconnect = await v.click('reconnect');
    assert.equal(v.rfbs.length, 1);
    assert.equal(v.element('connection-status').textContent, 'Connecting');
    v.sockets[0].onclose({});
    await reconnect.pending; await flush();
    assert.equal(v.rfbs.length, 2);
    assert.equal(v.rfbs[1].viewOnly, true);
});

test('old HTTP status cannot detach a replacement screen with a different instance', async () => {
    const v = await viewer({holdStatus: true}); v.connect();
    v.rfbs[0].listeners.disconnect({detail: {clean: false}});
    await v.click('reconnect'); v.connect(1, 'replacement-instance');
    v.statuses[0].resolve(response({state: 'running', instance_id: 'instance'}));
    await flush();
    assert.equal(v.rfbs[1].disconnected, undefined);
    assert.equal(v.element('connection-status').textContent, 'Connected');
    assert.equal(v.rfbs[1].viewOnly, true);
});


test('explicit reconnect still waits when an earlier stream error initiated close', async () => {
    const v = await viewer({holdClose: true}); v.connect(); await flush();
    v.rfbs[0].channel.onEvent({type: 'error', message: 'Transport unavailable'});
    const reconnect = await v.click('reconnect');
    assert.equal(v.rfbs.length, 1, 'earlier detach must retain the closing socket barrier');
    v.sockets[0].onclose({});
    await reconnect.pending; await flush();
    assert.equal(v.rfbs.length, 2);
    assert.equal(v.rfbs[1].viewOnly, true);
});

test('native noVNC failure retains the closing socket barrier and invalidates old callbacks', async () => {
    const v = await viewer({holdClose: true}); v.connect(); await flush();
    const takeover = await v.click('take-control');
    const failed = v.rfbs[0];
    // noVNC _fail() begins closing its channel, then emits disconnect without
    // waiting for the WebSocket close handshake to complete.
    failed.channel.close();
    failed.listeners.disconnect({detail: {clean: false}});
    const failureMessage = v.element('message').textContent;
    v.controls[0].resolve(response({message: 'Late takeover response'}, 503));
    await takeover.pending; await flush();
    assert.equal(v.element('message').textContent, failureMessage, 'old HTTP response cannot replace the disconnect explanation');
    failed.channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'late-revision', controller: true, readOnly: false});
    assert.equal(v.element('control-status').textContent, 'Read-only');
    const reconnect = await v.click('reconnect');
    assert.equal(v.sockets.length, 1, 'native disconnect must retain the old socket until its close event');
    assert.equal(v.element('connection-status').textContent, 'Connecting');
    v.sockets[0].onclose({});
    await reconnect.pending; await flush();
    assert.equal(v.sockets.length, 2);
    v.connect();
    assert.equal(v.rfbs[1].viewOnly, true);
    assert.equal(v.controls.length, 1, 'reconnect must not replay a takeover');
});

for (const timeoutFirst of [false, true]) test(`current screen confirms control ${timeoutFirst ? 'after' : 'before'} HTTP timeout`, async () => {
    const v = await viewer(); v.connect(); await flush();
    const takeover = await v.click('take-control');
    const confirm = () => v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'confirmed', controller: true, readOnly: false});
    if (!timeoutFirst) confirm();
    v.controls[0].resolve(response({message: 'The browser operation timed out; its outcome may be unknown.'}, 504));
    await takeover.pending; await flush();
    if (timeoutFirst) {
        assert.equal(v.rfbs[0].viewOnly, true, 'timeout alone must not grant control');
        assert.match(v.element('message').textContent, /outcome may be unknown/);
        confirm();
    }
    assert.equal(v.element('control-status').textContent, 'Controller');
    assert.match(v.element('message').textContent, /You control/);
    assert.equal(v.controls.length, 1, 'never retry automatically');
});

test('control POST pins the clicked attachment and reconnect aborts its request', async () => {
    const v = await viewer(); v.connect(); await flush();
    const takeover = await v.click('take-control');
    const request = v.requests.find(r => r.url === 'api/control');
    assert.deepEqual(JSON.parse(request.body), {revision: 'revision-0', attachment: 'attachment-0', instance: 'instance'});
    await v.click('reconnect'); v.connect(); await flush();
    assert.equal(request.signal.aborted, true);
    v.controls[0].resolve(response({})); await takeover.pending;
    assert.equal(v.rfbs[1].viewOnly, true);
});

test('replacement status owns its refresh slot even when an aborted older request completes', async () => {
    const v = await viewer({holdStatus: true}); v.connect(); await flush();
    const oldRequest = v.requests.find(r => r.url === 'api/status');
    await v.click('reconnect'); v.connect(1, 'replacement'); await flush();
    assert.equal(oldRequest.signal?.aborted, true);
    assert.equal(v.statuses.length, 2, 'replacement can refresh while old fetch cancellation completes');
    v.statuses[0].resolve(response({state: 'running', instance_id: 'old-instance'})); await flush();
    await v.click('refresh-status');
    assert.equal(v.statuses.length, 2, 'old finally must not release the new in-flight slot');
    v.statuses[1].resolve(response({state: 'running', instance_id: 'replacement'})); await flush();
    assert.equal(v.rfbs[1].disconnected, undefined);
    await v.click('refresh-status');
    assert.equal(v.statuses.length, 3);
});

test('HTTP success and status snapshot never grant control without a current stream event', async () => {
    const v = await viewer({holdStatus: true}); v.connect(); await flush();
    const takeover = await v.click('take-control');
    v.controls[0].resolve(response({controller_id: 'attachment-0', control_revision: 'new'})); await takeover.pending;
    v.statuses[0].resolve(response({state: 'running', instance_id: 'instance', controller_id: 'attachment-0', control_revision: 'new'})); await flush();
    assert.equal(v.rfbs[0].viewOnly, true);
    assert.equal(v.element('control-status').textContent, 'Read-only');
    assert.match(v.element('message').textContent, /waiting for the screen/);
});

test('control events from a different attachment cannot authorize the current screen', async () => {
    const v = await viewer(); v.connect(); await flush();
    for (const scope of [{attachment: 'other', instance: 'instance'}, {attachment: 'attachment-0', instance: 'other'}]) {
        assert.throws(() => v.rfbs[0].channel.onEvent({type: 'control', ...scope, revision: 'foreign', controller: true, readOnly: false}), /another attachment/);
        assert.equal(v.rfbs[0].viewOnly, true);
        assert.equal(v.element('control-status').textContent, 'Read-only');
    }
});

test('late HTTP failure cannot restore control after a confirmed takeover was revoked', async () => {
    const v = await viewer(); v.connect(); await flush();
    const takeover = await v.click('take-control');
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'confirmed', controller: true, readOnly: false});
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'revoked', controller: false, readOnly: true});
    v.controls[0].resolve(response({message: 'The browser operation timed out; its outcome may be unknown.'}, 504));
    await takeover.pending; await flush();
    assert.equal(v.rfbs[0].viewOnly, true);
    assert.equal(v.element('control-status').textContent, 'Read-only');
    assert.match(v.element('message').textContent, /confirmed.*now read-only/);
    assert.equal(v.controls.length, 1);
});

test('stream revocation updates confirmed control text after HTTP already completed', async () => {
    const v = await viewer(); v.connect(); await flush();
    const takeover = await v.click('take-control');
    v.controls[0].resolve(response({})); await takeover.pending;
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'confirmed', controller: true, readOnly: false});
    assert.match(v.element('message').textContent, /You control/);
    v.rfbs[0].channel.onEvent({type: 'control', attachment: 'attachment-0', instance: 'instance', revision: 'revoked', controller: false, readOnly: true});
    assert.equal(v.rfbs[0].viewOnly, true);
    assert.equal(v.element('control-status').textContent, 'Read-only');
    assert.match(v.element('message').textContent, /confirmed.*now read-only/);
    assert.equal(v.controls.length, 1);
});
