"use strict";

(async () => {
    const status = document.getElementById("bootstrap-status");
    const token = window.location.hash.slice(1);
    // The credential is a fragment: it never reaches HTTP, access logs, or a
    // Referer. Remove it before any request, and never persist it in web storage.
    window.history.replaceState(null, "", window.location.pathname);
    if (!/^[A-Za-z0-9_-]{43}$/.test(token)) {
        status.textContent = "No valid one-use viewer link. Open a new viewer from shiki.";
        return;
    }
    try {
        const response = await fetch("/bootstrap", {
            method: "POST",
            credentials: "same-origin",
            cache: "no-store",
            redirect: "error",
            headers: {"Content-Type": "application/json", "X-Shiki-Viewer": "bootstrap"},
            body: JSON.stringify({token}),
        });
        if (!response.ok) {
            status.textContent = "This one-use viewer link expired, was already used, or could not be authorized. Open a new viewer from shiki.";
            return;
        }
        const session = await response.json();
        if (!/^\/v\/[A-Za-z0-9_-]{22}\/$/.test(session.path)) {
            throw new Error("Invalid viewer session response");
        }
        window.location.replace(session.path);
    } catch {
        status.textContent = "The local viewer could not be reached or returned an unknown failure. Check that the browser client plugin is running and open a new viewer from shiki.";
    }
})();
