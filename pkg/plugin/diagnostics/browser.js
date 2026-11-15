async function(config) {
  "use strict";
  if (location.href !== config.targetURL || config.targetURL !== "about:blank#shikigami-diagnostics-v1") {
    throw new Error("diagnostics require the managed server Chrome dedicated target");
  }
  const source = "fixed diagnostic JavaScript in managed server Chrome";
  const vantage = "server-chrome:" + config.profileId;
  const started = Math.floor(Date.now() / 1000);
  const deadline = Date.now() + 28000;
  const report = { observations: [], warnings: [], observedAt: started };
  const add = (check, state, value, limitation, origin = source, view = vantage) => {
    const text = typeof value === "string" ? value : JSON.stringify(value);
    report.observations.push({ check, state, value: text, expected: "", source: origin,
      vantage: view, observedAt: Math.floor(Date.now() / 1000), limitation });
  };
  const native = (check, get, limitation) => {
    try { add(check, "observed", get(), limitation); }
    catch (_) { add(check, "unknown", "", "Native browser API unavailable or rejected the bounded inspection. " + limitation); }
  };
  add("browser.context", "observed", { profile_id: config.profileId, target_url: config.targetURL },
    "Dedicated diagnostic target in the managed server Chrome process and original profile context, not the client machine or noVNC browser. Runtime must verify its private CDP pipe and target identity; these labels are not cryptographic authentication.");
  native("browser.intl", () => ({ date_time: Intl.DateTimeFormat().resolvedOptions(), number: Intl.NumberFormat().resolvedOptions(),
    timezone_offset_minutes: new Date().getTimezoneOffset() }),
    "Native Intl and Date values at inspection time; no locale/timezone overrides or identity claims are applied.");
  native("browser.languages", () => ({ language: navigator.language, languages: Array.from(navigator.languages || []) }),
    "Native navigator language preferences; HTTP Accept-Language may differ and is observed only if an approved endpoint echoes it.");
  native("browser.request.native", () => ({ user_agent: navigator.userAgent, platform: navigator.platform,
    user_agent_data: navigator.userAgentData ? navigator.userAgentData.toJSON() : null,
    cookie_enabled: navigator.cookieEnabled, webdriver: navigator.webdriver,
    do_not_track: navigator.doNotTrack, global_privacy_control: navigator.globalPrivacyControl ?? null }),
    "Local native request-related API facts, not a captured TLS fingerprint or actual HTTP header trace. Cookie contents, credentials, high-entropy client hints, account data, and profile files are not read. Telemetry preferences are not changed.");
  native("browser.network.native", () => ({ online: navigator.onLine,
    connection: navigator.connection ? { effective_type: navigator.connection.effectiveType, rtt: navigator.connection.rtt,
      downlink: navigator.connection.downlink, save_data: navigator.connection.saveData } : null }),
    "Native browser connectivity hints are not proof of Internet reachability, egress family, proxy absence, or recursive DNS behavior.");
  add("browser.secure-dns.native", "unknown", "",
    "Page JavaScript cannot inspect the target profile's effective Chrome Secure DNS setting or resolver source IP. Runtime configuration evidence and actual authoritative callbacks must be reported separately.");
  native("browser.canvas", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 2; canvas.height = 2;
    const ctx = canvas.getContext("2d");
    return { context_2d_available: !!ctx, attributes: ctx && ctx.getContextAttributes ? ctx.getContextAttributes() : null };
  }, "Only native Canvas capability/context facts are read. No image, pixel fingerprint, hash, spoof, or fingerprint upload is produced.");
  native("browser.webgl", () => {
    const canvas = document.createElement("canvas");
    const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
    if (!gl) return { available: false };
    const debug = gl.getExtension("WEBGL_debug_renderer_info");
    const facts = { available: true, version: gl.getParameter(gl.VERSION), vendor: gl.getParameter(gl.VENDOR),
      renderer: gl.getParameter(gl.RENDERER), shading_language: gl.getParameter(gl.SHADING_LANGUAGE_VERSION),
      unmasked_vendor: debug ? gl.getParameter(debug.UNMASKED_VENDOR_WEBGL) : null,
      unmasked_renderer: debug ? gl.getParameter(debug.UNMASKED_RENDERER_WEBGL) : null };
    const lose = gl.getExtension("WEBGL_lose_context");
    if (lose) lose.loseContext();
    return facts;
  }, "Native local graphics facts only; software rendering and unavailable APIs remain facts, not identity or risk verdicts. No spoof, rendered image, hash, or upload.");

  if (config.allowWebRTC) {
    try {
      if (!globalThis.RTCPeerConnection) throw new Error("unavailable");
      const peer = new RTCPeerConnection({ iceServers: [], iceCandidatePoolSize: 0 });
      const candidates = [];
      let finish;
      let timer;
      const complete = new Promise(resolve => {
        finish = resolve;
        timer = setTimeout(resolve, 1500);
        peer.onicecandidate = event => {
          if (!event.candidate) { resolve(); return; }
          if (candidates.length < 16) candidates.push(String(event.candidate.candidate).slice(0, 512));
        };
      });
      try {
        peer.createDataChannel("diagnostic-local-only");
        await Promise.race([(async () => {
          const offer = await peer.createOffer();
          await peer.setLocalDescription(offer);
        })(), complete]);
        await complete;
        add("browser.webrtc", candidates.length ? "observed" : "unknown", { ice_servers: [], candidates },
          "Explicit opt-in local ICE gathering without STUN, TURN, remote peer, media access, or third-party fallback. Candidates may be mDNS-obfuscated; no candidates does not prove no leak, NAT behavior, or public WebRTC egress.");
      } finally { clearTimeout(timer); finish(); peer.close(); }
    } catch (_) { add("browser.webrtc", "unknown", "", "Explicit opt-in native ICE gathering was unavailable or failed; no fallback or leak-free pass is inferred."); }
  } else {
    add("browser.webrtc", "unknown", "", "Not authorized: WebRTC candidate gathering is off by default. No STUN/TURN service or peer was contacted.");
  }

  const providers = config.providers || [];
  if (!config.allowExternal) {
    add("external.authorization", "observed", "disabled", "No external fetch, DNS-observer challenge, geo endpoint, or hidden supplier was contacted.");
    add("dns.authoritative", "unknown", "", "External DNS observation was not authorized; browser language and local DNS files cannot substitute for authoritative callbacks.");
    return JSON.stringify(report);
  }
  add("external.authorization", "observed", "explicitly enabled",
    "Only approved HTTPS providers and the versioned DNS challenge are contacted. They receive connection IP, TLS and ordinary native browser headers (including browser-controlled client hints where applicable). Credentials, cookies, referrers, report, graphics facts, and composite fingerprints are not sent.");
  add("browser.network.safety", "warning", "native profile network path preserved",
    "Runtime requires exact-URL CDP request gating, public-DNS preflight, redirect rejection, and observed remote-IP checks when available. Native Chrome cannot pin a page request's pre-connect address: trusted hostname providers retain a DNS-rebinding race. Post-connect checks are not prevention. No proxy, DNS override, host fetch substitution, or security pass is used.");
  add("browser.remote-address.api", "unknown", "",
    "Page JavaScript cannot observe the connected remote IP or independently prove an egress family. Runtime CDP evidence is separate; absent remote-IP evidence must remain unknown.");
  if (!providers.length) add("external.providers", "unknown", "", "No provider configured; authorization never silently selects a public supplier.");

  const boundedFetch = async (url, options = {}, parseJSON = true) => {
    const remaining = deadline - Date.now();
    if (remaining <= 0) throw new Error("inspection deadline");
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), Math.min(5000, remaining));
    let reader;
    try {
      const response = await fetch(url, { ...options, credentials: "omit", redirect: "error", referrerPolicy: "no-referrer",
        cache: "no-store", mode: "cors", signal: controller.signal,
        headers: { Accept: "application/json", ...(options.body ? { "Content-Type": "application/json" } : {}) } });
      if (!response.ok || response.redirected) throw new Error("provider response");
      if (parseJSON && !/^application\/(?:json|[a-z0-9.+-]+\+json)(?:\s*;|$)/i.test(response.headers.get("content-type") || "")) throw new Error("provider content type");
      if (!response.body) throw new Error("provider body missing");
      reader = response.body.getReader();
      const chunks = [];
      let size = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > (parseJSON ? 16384 : 512)) throw new Error("provider body limit");
        chunks.push(value);
      }
      if (!parseJSON) return null;
      const bytes = new Uint8Array(size);
      let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
      const value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
      if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("provider object required");
      return value;
    } finally {
      clearTimeout(timer);
      controller.abort();
      if (reader) { try { await reader.cancel(); } catch (_) {} }
    }
  };
  const ipInfo = raw => {
    if (typeof raw !== "string" || raw.length > 45 || raw.includes("%")) return null;
    const v4 = raw.split(".");
    if (v4.length === 4 && v4.every(part => /^(0|[1-9][0-9]{0,2})$/.test(part) && Number(part) < 256)) {
      const bytes = v4.map(Number);
      const [a, b, c] = bytes;
      const publicIP = !(a === 0 || a === 10 || a === 127 || a >= 224 || a === 100 && b >= 64 && b <= 127 ||
        a === 169 && b === 254 || a === 172 && b >= 16 && b <= 31 || a === 192 && (b === 0 && (c === 0 || c === 2) || b === 88 && c === 99 || b === 168) ||
        a === 198 && (b === 18 || b === 19 || b === 51 && c === 100) || a === 203 && b === 0 && c === 113);
      return { family: "ipv4", public: publicIP };
    }
    if (!raw.includes(":") || !/^[a-f0-9:.]+$/i.test(raw)) return null;
    try {
      const canonical = new URL("https://[" + raw + "]/").hostname.slice(1, -1);
      const halves = canonical.split("::");
      const left = halves[0] ? halves[0].split(":") : [];
      const right = halves.length > 1 && halves[1] ? halves[1].split(":") : [];
      const groups = halves.length === 1 ? left.map(x => parseInt(x, 16)) : [...left.map(x => parseInt(x, 16)), ...Array(8 - left.length - right.length).fill(0), ...right.map(x => parseInt(x, 16))];
      if (groups.length !== 8) return null;
      if (groups.slice(0, 5).every(x => x === 0) && groups[5] === 65535) return ipInfo([groups[6] >> 8, groups[6] & 255, groups[7] >> 8, groups[7] & 255].join("."));
      return { family: "ipv6", public: groups[0] >= 0x2000 && groups[0] <= 0x3fff &&
        !(groups[0] === 0x2001 && (groups[1] <= 0x1ff || groups[1] === 0xdb8)) && groups[0] !== 0x2002 &&
        !(groups[0] === 0x3fff && groups[1] <= 0xfff) };
    } catch (_) { return null; }
  };
  const observerView = provider => "external-observer:" + provider.id + ":" + vantage;
  const observerSource = provider => "provider " + provider.id + " (" + provider.kind + "), " + provider.url;
  let hasDNS = false;
  for (const provider of providers) {
    add("external.provider", "observed", provider,
      "Explicit supplier and outgoing fields are declared in the authorization observation; no provider endorsement or hidden fallback.");
    const view = observerView(provider);
    const origin = observerSource(provider);
    try {
      if (provider.kind === "ip-echo") {
        const raw = await boundedFetch(provider.url);
        const ip = ipInfo(raw.ip);
        if (!ip) throw new Error("literal source IP missing");
        const mismatch = provider.ipFamily && ip.family !== provider.ipFamily;
        add("https.egress." + (provider.ipFamily || ip.family), mismatch ? "unknown" : ip.public ? "observed" : "warning", raw,
          mismatch ? "Provider claimed a different family from configured intent. Raw data retained, not a successful family probe; Chrome routing was not forced." :
          "Raw provider-attributed source IP and optional echoed request metadata from native Chrome. No family was forced and no IP geolocation or consistency pass is inferred. A non-public claimed source is only an untrusted warning.", origin, view);
      } else if (provider.kind === "geo") {
        const raw = await boundedFetch(provider.url);
        add("geo.raw", "observed", raw, "Raw provider data retained independently, including conflicts. No combined country verdict, reputation, account eligibility, provider endorsement, or risk score is calculated.", origin, view);
      } else if (provider.kind === "dns-observer") {
        hasDNS = true;
        const random = crypto.getRandomValues(new Uint8Array(16));
        const nonce = Array.from(random, x => x.toString(16).padStart(2, "0")).join("");
        const hostname = nonce + "." + provider.dnsZone;
        const sessionStart = Math.floor(Date.now() / 1000);
        const endpoint = provider.url.replace(/\/+$/, "") + "/sessions";
        const session = await boundedFetch(endpoint, { method: "POST", body: JSON.stringify({ version: 1, nonce }) });
        if (session.version !== 1 || typeof session.id !== "string" || !/^[A-Za-z0-9_-]{1,96}$/.test(session.id) || session.hostname !== hostname) throw new Error("invalid DNS session");
        // The response may fail CORS, TLS, or HTTP; a DNS query can still have
        // reached the authoritative observer. Only a fresh callback is evidence.
        try { await boundedFetch("https://" + hostname + "/", {}, false); } catch (_) {}
        let callbacks = null;
        for (let attempt = 0; attempt < 3; attempt++) {
          if (attempt) await new Promise(resolve => setTimeout(resolve, 300));
          const result = await boundedFetch(endpoint + "/" + session.id);
          if (result.version !== 1 || result.id !== session.id) throw new Error("invalid DNS result");
          if (result.complete !== true) continue;
          if (!Array.isArray(result.queries) || !result.queries.length || result.queries.length > 32) throw new Error("empty DNS observation");
          const now = Math.floor(Date.now() / 1000);
          for (const query of result.queries) {
            if (query.hostname !== hostname || !ipInfo(query.resolver_ip) || !["A", "AAAA", "HTTPS", "SVCB"].includes(query.qtype) ||
              !Number.isInteger(query.observed_at) || query.observed_at < sessionStart - 5 || query.observed_at > now + 5) throw new Error("mismatched or stale DNS callback");
            if (query.ecs) {
              const [address, bits, extra] = String(query.ecs).split("/");
              const ip = ipInfo(address);
              if (extra !== undefined || !ip || !/^[0-9]{1,3}$/.test(bits || "") || Number(bits) > (ip.family === "ipv4" ? 32 : 128)) throw new Error("invalid ECS evidence");
            }
          }
          callbacks = result.queries;
          break;
        }
        if (!callbacks) throw new Error("DNS observer incomplete");
        add("dns.authoritative", "observed", { hostname, queries: callbacks },
          "Fresh matching callbacks from the explicitly configured authoritative observer, triggered in the target Chrome's unchanged native network path. Resolver source/ECS are raw observer claims, separate from configured Secure DNS, HTTPS egress, fixed geography, or a claim that all DNS follows this path.", origin, view);
      }
    } catch (_) {
      add(provider.kind === "dns-observer" ? "dns.authoritative" : provider.kind === "geo" ? "geo.raw" : "https.egress." + (provider.ipFamily || "native"), "unknown", "",
        "Approved endpoint unavailable, unsafe, blocked by CORS/TLS/CDP, timed out, oversized, malformed, mismatched, or lacked a fresh observer callback. Unknown is not a pass. No host substitute, invented IP, or fallback supplier was used.", origin, view);
    }
  }
  if (!hasDNS) add("dns.authoritative", "unknown", "", "No explicit DNS observer configured. Secure DNS configuration, HTTPS IP, and native APIs do not reveal an authoritative observer's actual recursive source or ECS.");
  report.warnings.push("Native Chrome hostname probes retain a pre-connect DNS-rebinding race despite public-DNS preflight and post-connect checks. Use only explicitly trusted providers; diagnostics do not sandbox same-UID Chrome.");
  report.warnings.push("Provider claims may conflict. No identity, residency, service eligibility, Claude-approved reputation, or anti-ban score is inferred; no telemetry setting was changed.");
  return JSON.stringify(report);
}
