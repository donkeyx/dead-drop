(function () {
  "use strict";

  const byId = (id) => document.getElementById(id);
  const apiPath = () => "/api/v1/secrets/" + encodeURIComponent(location.pathname.split("/").pop());
  const maxPlaintextBytes = 16 * 1024 * 1024;

  function setMessage(node, message) {
    node.replaceChildren(document.createTextNode(message));
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
    setMessage(result, "Encrypting in this browser...");
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
      const response = await fetch("/api/v1/secrets", {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream", "X-Seal-Burn": byId("burn").checked ? "1" : "0" },
        body: sealed.blob
      });
      if (!response.ok) throw new Error("server rejected the encrypted drop");
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
        } catch (error) {
          copy.textContent = error.message;
        }
      }, { once: true });
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
    setMessage(result, "Downloading encrypted drop...");
    try {
      const response = await fetch(apiPath(), { cache: "no-store" });
      if (!response.ok) throw new Error(response.status === 404 ? "Drop not found or already burned." : "Download failed.");
      const sealed = new Uint8Array(await response.arrayBuffer());
      const opened = await DeadDrop.decrypt(sealed, key, byId("reveal-passphrase").value);
      result.replaceChildren();
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

  byId("create-form")?.addEventListener("submit", createDrop);
  byId("open-drop")?.addEventListener("click", revealDrop);
  document.querySelectorAll("button").forEach((button) => {
    button.dataset.label = button.textContent;
  });
  if (location.pathname.startsWith("/s/")) {
    byId("create-panel").hidden = true;
    byId("reveal-panel").hidden = false;
  }
})();
