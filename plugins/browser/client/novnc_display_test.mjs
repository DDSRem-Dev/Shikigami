import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';

// Exercise the vendored Display with controllable asynchronous image decoding.
// The canvas double snapshots the image at draw time: releasing its source must
// neither erase already drawn pixels nor allow later images to overtake it.
const source = (await readFile(new URL('./web/novnc/core/display.js', import.meta.url), 'utf8'))
    .replace(/^import .*;$/gm, '')
    .replace('export default class Display', 'globalThis.Display = class Display');

function displayHarness() {
    const images = [], events = [], errors = [];
    class DecodedImage {
        complete = false; width = 1; height = 1; listeners = new Map();
        constructor() { images.push(this); }
        get src() { return this.source; }
        set src(value) {
            this.source = value;
            if (value) this.pixel = Buffer.from(value.split(',')[1], 'base64')[0];
            else events.push(`release:${this.pixel}`);
        }
        addEventListener(name, listener) { this.listeners.set(name, listener); }
        removeEventListener(name, listener) {
            assert.equal(this.listeners.get(name), listener);
            this.listeners.delete(name);
        }
        decode() {
            this.complete = true;
            this.listeners.get('load')?.call(this);
        }
    }
    function canvas(visible) {
        const surface = {width: 1, height: 1, style: {}, pixel: undefined};
        surface.getContext = () => ({drawImage(image) {
            if (image instanceof DecodedImage) assert.ok(image.src, 'image released before drawing');
            surface.pixel = image.pixel;
            events.push(`${visible ? 'present' : 'draw'}:${surface.pixel}`);
        }});
        return surface;
    }
    const target = canvas(true);
    const context = vm.createContext({
        Image: DecodedImage, document: {createElement: () => canvas(false)},
        navigator: {userAgent: 'display-contract-test'},
        Log: {Debug() {}, Warn() {}, Error(message) { errors.push(message); }},
        Base64: {encode: bytes => Buffer.from(bytes).toString('base64')},
        toSigned32bit: value => value | 0,
    });
    vm.runInContext(source, context);
    const display = new context.Display(target);
    const enqueue = pixel => display.imageRect(0, 0, 1, 1, 'image/png', new Uint8Array([pixel]));
    return {display, target, images, events, errors, enqueue};
}

test('decoded images retain ordering and visible pixels while releasing sources', async () => {
    const h = displayHarness();
    h.enqueue(11);
    h.display.flip();
    h.enqueue(22);
    h.display.flip();
    let flushed = false;
    const pending = h.display.flush().then(() => { flushed = true; });

    // A later image finishes decoding first, but cannot be painted early.
    h.images[1].decode();
    await Promise.resolve();
    assert.equal(flushed, false);
    assert.equal(h.target.pixel, undefined);
    assert.ok(h.images.every(image => image.src));
    h.images[0].decode();
    await pending;

    assert.deepEqual(h.events, ['draw:11', 'release:11', 'present:11', 'draw:22', 'release:22', 'present:22']);
    assert.equal(h.target.pixel, 22);
    assert.ok(h.images.every(image => image.src === '' && image.listeners.size === 0));
    assert.equal(h.display.pending(), false);
    assert.deepEqual(h.errors, []);
});

test('successive decoded frames do not retain encoded image sources', async () => {
    const h = displayHarness();
    for (let pixel = 1; pixel <= 64; pixel++) {
        h.enqueue(pixel);
        h.display.flip();
        const pending = h.display.flush();
        h.images.at(-1).decode();
        await pending;
        assert.equal(h.target.pixel, pixel);
        assert.equal(h.display.pending(), false);
        assert.ok(h.images.every(image => image.src === '' && image.listeners.size === 0));
    }
    assert.equal(h.events.filter(event => event.startsWith('present:')).length, 64);
    assert.deepEqual(h.errors, []);
});

test('invalid image dimensions are rejected before painting or releasing data', () => {
    const h = displayHarness();
    h.enqueue(7);
    h.display.flip();
    h.images[0].width = 2;
    h.images[0].decode();
    assert.equal(h.target.pixel, undefined);
    assert.ok(h.images[0].src);
    assert.equal(h.errors.length, 1);
    assert.match(h.errors[0], /incorrect dimensions/);
    assert.equal(h.display.pending(), true);
    assert.deepEqual(h.events, []);
});

test('JPEG marker scanning preserves bytes across every transport split', async () => {
    const context = vm.createContext({
        WebSocket: {CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3},
        Log: {Debug() {}},
    });
    for (const [path, name] of [['core/websock.js', 'Websock'], ['core/decoders/jpeg.js', 'JPEGDecoder']]) {
        const text = (await readFile(new URL(`./web/novnc/${path}`, import.meta.url), 'utf8'))
            .replace(/^import .*;$/gm, '')
            .replace(`export default class ${name}`, `globalThis.${name} = class ${name}`);
        vm.runInContext(text, context);
    }
    // A framing fixture, not a browser-decodable image: include both escaped
    // 0xff and restart markers so neither is mistaken for the end of a scan.
    const wire = new Uint8Array([0xff, 0xd8, 0xff, 0xc0, 0, 2, 0xff, 0xda, 0, 2,
        0x11, 0xff, 0, 0x22, 0xff, 0xd0, 0x33, 0xff, 0xd9]);
    for (let split = 1; split < wire.length; split++) {
        const socket = new context.Websock(), decoder = new context.JPEGDecoder(), rendered = [];
        socket.init();
        const display = {imageRect(x, y, width, height, mime, bytes) {
            rendered.push({x, y, width, height, mime, bytes: [...bytes]});
        }};
        socket._recvMessage({data: wire.slice(0, split).buffer});
        assert.equal(decoder.decodeRect(1, 2, 3, 4, socket, display, 24), false);
        assert.equal(rendered.length, 0);
        socket._recvMessage({data: wire.slice(split).buffer});
        assert.equal(decoder.decodeRect(1, 2, 3, 4, socket, display, 24), true);
        assert.deepEqual(rendered, [{x: 1, y: 2, width: 3, height: 4, mime: 'image/jpeg', bytes: [...wire]}]);
        assert.equal(socket.rQlen(), 0);
    }
});
