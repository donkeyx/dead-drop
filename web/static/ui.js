(function () {
  "use strict";

  const byId = (id) => document.getElementById(id);
  const apiPath = () => "/api/v1/secrets/" + encodeURIComponent(location.pathname.split("/").pop());
  const maxPlaintextBytes = 16 * 1024 * 1024;
  const turnstileSiteKey = document.documentElement.dataset.turnstileSitekey || "";
  let turnstileWidget = null;

  function setMessage(node, message, state = "error") {
    node.replaceChildren(document.createTextNode(message));
    node.className = state;
  }

  function setBusy(button, busy, label) {
    button.disabled = busy;
    button.textContent = busy ? label : button.dataset.label;
  }

  async function copyText(text) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const input = document.createElement("textarea");
    input.value = text;
    input.setAttribute("readonly", "");
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.append(input);
    input.select();
    if (!document.execCommand("copy")) throw new Error("clipboard access is unavailable");
    input.remove();
  }

  async function createDrop(event) {
    event.preventDefault();
    const result = byId("create-result");
    const button = event.target.querySelector("button[type=submit]");
    const file = byId("file").files[0];
    const text = byId("secret").value;
    if (!file && !text) {
      setMessage(result, "Enter a secret or choose a file first.");
      return;
    }
    setBusy(button, true, "Encrypting...");
    setMessage(result, "Encrypting in this browser...", "progress");
    try {
      const passphrase = byId("passphrase").value;
      const plaintext = file ? new Uint8Array(await file.arrayBuffer()) : new TextEncoder().encode(text);
      if (plaintext.byteLength > maxPlaintextBytes) {
        throw new Error("The selected content is too large (maximum 16 MiB).");
      }
      const sealed = await DeadDrop.encrypt(plaintext, {
        passphrase,
        filename: file ? file.name : "",
        contentType: file ? (file.type || "application/octet-stream") : "text/plain; charset=utf-8"
      });
      const headers = { "Content-Type": "application/octet-stream", "X-Seal-Burn": byId("burn").checked ? "1" : "0" };
      const token = await turnstileToken();
      if (token) headers["CF-Turnstile-Response"] = token;
      const response = await fetch("/api/v1/secrets", {
        method: "POST",
        headers,
        body: sealed.blob
      });
      if (!response.ok) {
        let message = "server rejected the encrypted drop";
        try {
          const failed = await response.json();
          if (failed.error === "human_check") message = "Human check failed. Reload and try again.";
          else if (failed.error === "rate_limit") message = "Too many creates. Wait and try again.";
        } catch (_) { /* keep default */ }
        throw new Error(message);
      }
      if (window.turnstile && turnstileWidget !== null) window.turnstile.reset(turnstileWidget);
      const created = await response.json();
      const link = location.origin + created.path + "#" + sealed.key;
      result.replaceChildren();
      const label = document.createElement("strong");
      label.textContent = "Share link";
      const row = document.createElement("div");
      row.className = "link-row";
      const linkInput = document.createElement("input");
      linkInput.type = "text";
      linkInput.readOnly = true;
      linkInput.value = link;
      linkInput.setAttribute("aria-label", "Share link");
      const copy = document.createElement("button");
      copy.type = "button";
      copy.dataset.label = "Copy";
      copy.textContent = copy.dataset.label;
      copy.addEventListener("click", async () => {
        try {
          await copyText(link);
          copy.textContent = "Copied";
          window.setTimeout(() => { copy.textContent = copy.dataset.label; }, 1800);
        } catch (error) {
          copy.textContent = error.message;
        }
      });
      row.append(linkInput, copy);
      result.append(label, row);
      byId("secret").value = "";
      byId("file").value = "";
      byId("passphrase").value = "";
    } catch (error) {
      setMessage(result, error.message);
    } finally {
      setBusy(button, false, "Encrypting...");
    }
  }

  async function revealDrop() {
    const result = byId("reveal-result");
    const key = location.hash.slice(1);
    if (!key) { setMessage(result, "This link has no fragment key."); return; }
    history.replaceState(null, "", location.pathname);
    const button = byId("open-drop");
    setBusy(button, true, "Opening...");
    setMessage(result, "Downloading encrypted drop...", "progress");
    try {
      const response = await fetch(apiPath(), { cache: "no-store" });
      if (!response.ok) throw new Error(response.status === 404 ? "Drop not found or already burned." : "Download failed.");
      const sealed = new Uint8Array(await response.arrayBuffer());
      const opened = await DeadDrop.decrypt(sealed, key, byId("reveal-passphrase").value);
      result.replaceChildren();
      result.className = "success";
      if (opened.filename) {
        const download = document.createElement("a");
        const objectURL = URL.createObjectURL(new Blob([opened.plaintext], { type: opened.contentType || "application/octet-stream" }));
        download.href = objectURL;
        download.download = opened.filename;
        download.textContent = "Download " + opened.filename;
        download.addEventListener("click", () => {
          setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
        }, { once: true });
        result.append(download);
      } else {
        const pre = document.createElement("pre");
        pre.textContent = new TextDecoder().decode(opened.plaintext);
        result.append(pre);
      }
    } catch (error) {
      setMessage(result, error.message);
    } finally {
      setBusy(button, false, "Opening...");
    }
  }

  function loadTurnstile() {
    return new Promise((resolve, reject) => {
      if (window.turnstile) {
        resolve();
        return;
      }
      const script = document.createElement("script");
      script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
      script.async = true;
      script.onload = () => resolve();
      script.onerror = () => reject(new Error("Could not load human check"));
      document.head.append(script);
    });
  }

  async function setupTurnstile() {
    if (!turnstileSiteKey) return;
    const box = byId("cf-turnstile");
    if (box) box.hidden = false;
    await loadTurnstile();
    if (turnstileWidget !== null) return;
    turnstileWidget = window.turnstile.render("#cf-turnstile", { sitekey: turnstileSiteKey });
  }

  async function turnstileToken() {
    if (!turnstileSiteKey) return "";
    await setupTurnstile();
    const token = window.turnstile.getResponse(turnstileWidget);
    if (!token) throw new Error("Complete the human check first.");
    return token;
  }

  byId("create-form")?.addEventListener("submit", createDrop);
  byId("open-drop")?.addEventListener("click", revealDrop);
  document.querySelectorAll("[data-toggle-visibility]").forEach((toggle) => {
    toggle.addEventListener("click", () => {
      const input = byId(toggle.dataset.toggleVisibility);
      const isSecret = input.tagName === "TEXTAREA";
      const visible = isSecret ? !input.classList.contains("privacy-mode") : input.type === "text";
      const nextVisible = !visible;
      if (isSecret) {
        input.classList.toggle("privacy-mode", !nextVisible);
      } else {
        input.type = nextVisible ? "text" : "password";
      }
      const label = nextVisible ? "Hide" : "Show";
      const name = isSecret ? "secret" : "passphrase";
      toggle.setAttribute("aria-label", label + " " + name);
      toggle.title = label + " " + name;
    });
  });
  document.querySelectorAll("button").forEach((button) => {
    button.dataset.label = button.textContent;
  });
  if (location.pathname.startsWith("/s/")) {
    byId("create-panel").hidden = true;
    byId("reveal-panel").hidden = false;
  } else {
    setupTurnstile().catch(() => {});
  }
})();
