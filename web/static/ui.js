(function () {
  "use strict";

  const byId = (id) => document.getElementById(id);
  const apiPath = () => "/api/v1/secrets/" + encodeURIComponent(location.pathname.split("/").pop());

  async function createDrop(event) {
    event.preventDefault();
    const result = byId("create-result");
    result.textContent = "Encrypting in this browser...";
    try {
      const text = byId("secret").value;
      const passphrase = byId("passphrase").value;
      const sealed = await DeadDrop.encrypt(new TextEncoder().encode(text), { passphrase, contentType: "text/plain; charset=utf-8" });
      const response = await fetch("/api/v1/secrets", {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream", "X-Seal-Burn": byId("burn").checked ? "1" : "0" },
        body: sealed.blob
      });
      if (!response.ok) throw new Error("server rejected the encrypted drop");
      const created = await response.json();
      const link = location.origin + created.path + "#" + sealed.key;
      result.textContent = "Share link: " + link;
      byId("secret").value = "";
      byId("passphrase").value = "";
    } catch (error) {
      result.textContent = error.message;
    }
  }

  async function revealDrop() {
    const result = byId("reveal-result");
    const key = location.hash.slice(1);
    if (!key) { result.textContent = "This link has no fragment key."; return; }
    history.replaceState(null, "", location.pathname);
    result.textContent = "Downloading encrypted drop...";
    try {
      const response = await fetch(apiPath(), { cache: "no-store" });
      if (!response.ok) throw new Error(response.status === 404 ? "Drop not found or already burned." : "Download failed.");
      const sealed = new Uint8Array(await response.arrayBuffer());
      const opened = await DeadDrop.decrypt(sealed, key, byId("reveal-passphrase").value);
      result.textContent = new TextDecoder().decode(opened.plaintext);
    } catch (error) {
      result.textContent = error.message;
    }
  }

  byId("create-form")?.addEventListener("submit", createDrop);
  byId("open-drop")?.addEventListener("click", revealDrop);
  if (location.pathname.startsWith("/s/")) {
    byId("create-panel").hidden = true;
    byId("reveal-panel").hidden = false;
  }
})();
