/**
 * dead-drop browser glue: load Go WASM and expose Promise-based encrypt/decrypt.
 * Expects web/static/wasm_exec.js and web/static/dead-drop.wasm (same origin).
 */
(function (global) {
  "use strict";

  let readyPromise = null;

  function defaultWasmURL() {
    const base = document.currentScript && document.currentScript.src
      ? document.currentScript.src.replace(/[^/]+$/, "")
      : "/static/";
    return base + "dead-drop.wasm";
  }

  /**
   * Load wasm_exec.js (must be included separately) + dead-drop.wasm.
   * @param {string} [wasmURL]
   * @returns {Promise<void>}
   */
  function ready(wasmURL) {
    if (readyPromise) return readyPromise;
    readyPromise = (async () => {
      if (typeof Go === "undefined") {
        throw new Error("wasm_exec.js not loaded (Go runtime missing)");
      }
      const go = new Go();
      const url = wasmURL || defaultWasmURL();
      const result = await WebAssembly.instantiateStreaming(fetch(url), go.importObject);
      go.run(result.instance);
      // wait until Go sets global deaddrop
      const deadline = Date.now() + 10000;
      while (Date.now() < deadline) {
        if (global.deaddrop && global.deaddrop.encrypt) return;
        await new Promise((r) => setTimeout(r, 10));
      }
      throw new Error("dead-drop WASM failed to register deaddrop.encrypt");
    })();
    return readyPromise;
  }

  /**
   * @param {Uint8Array} plaintext
   * @param {{ passphrase?: string, filename?: string, contentType?: string }} [options]
   * @returns {Promise<{ blob: Uint8Array, key: string, flags: number }>}
   */
  async function encrypt(plaintext, options) {
    await ready();
    const res = global.deaddrop.encrypt(plaintext, options || {});
    if (res && res.error) throw new Error(res.error);
    return res;
  }

  /**
   * @param {Uint8Array} sealed
   * @param {string} key base64url fragment key
   * @param {string} [passphrase]
   * @returns {Promise<{ plaintext: Uint8Array, filename: string, contentType: string }>}
   */
  async function decrypt(sealed, key, passphrase) {
    await ready();
    const res = global.deaddrop.decrypt(sealed, key, passphrase || "");
    if (res && res.error) throw new Error(res.error);
    return res;
  }

  global.DeadDrop = { ready, encrypt, decrypt };
})(typeof globalThis !== "undefined" ? globalThis : window);
