/**
 * Node harness: open PR1 golden vectors via dead-drop.wasm.
 * Usage (from repo root, after make wasm):
 *   node web/static/wasm_vectors.mjs
 */
import { readFileSync, existsSync } from "fs";
import { createRequire } from "module";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = join(__dirname, "../..");
const require = createRequire(import.meta.url);

// Polyfill enough of browser for wasm_exec.js under Node.
import { webcrypto } from "crypto";
if (!globalThis.crypto) globalThis.crypto = webcrypto;

// Minimal performance.now
if (!globalThis.performance) {
  globalThis.performance = { now: () => Date.now() };
}

// Load Go's wasm_exec
const wasmExec = join(__dirname, "wasm_exec.js");
if (!existsSync(wasmExec)) {
  console.error("missing wasm_exec.js — run: make wasm");
  process.exit(1);
}
require(wasmExec);

const wasmPath = join(__dirname, "dead-drop.wasm");
if (!existsSync(wasmPath)) {
  console.error("missing dead-drop.wasm — run: make wasm");
  process.exit(1);
}

function b64urlDecode(s) {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
  return Buffer.from(b64, "base64");
}

function loadJSON(name) {
  return JSON.parse(readFileSync(join(root, "blob/testdata", name), "utf8"));
}

async function main() {
  const go = new globalThis.Go();
  const buf = readFileSync(wasmPath);
  const result = await WebAssembly.instantiate(buf, go.importObject);
  // Don't await go.run — it never resolves (Go main blocks).
  go.run(result.instance);

  // wait for registration
  for (let i = 0; i < 500; i++) {
    if (globalThis.deaddrop?.decrypt) break;
    await new Promise((r) => setTimeout(r, 10));
  }
  if (!globalThis.deaddrop?.decrypt) {
    throw new Error("deaddrop not registered");
  }

  // nopass
  const g0 = loadJSON("v1_nopass.json");
  const blob0 = new Uint8Array(b64urlDecode(g0.blob_b64url));
  const r0 = globalThis.deaddrop.decrypt(blob0, g0.master_key_b64url, "");
  if (r0.error) throw new Error("nopass: " + r0.error);
  const pt0 = Buffer.from(r0.plaintext);
  const want0 = b64urlDecode(g0.plaintext_b64url);
  if (!pt0.equals(want0)) throw new Error("nopass plaintext mismatch");
  console.log("ok: v1_nopass");

  // passphrase
  const g1 = loadJSON("v1_passphrase.json");
  const blob1 = new Uint8Array(b64urlDecode(g1.blob_b64url));
  const r1 = globalThis.deaddrop.decrypt(blob1, g1.master_key_b64url, g1.passphrase);
  if (r1.error) throw new Error("pass: " + r1.error);
  const pt1 = Buffer.from(r1.plaintext);
  const want1 = b64urlDecode(g1.plaintext_b64url);
  if (!pt1.equals(want1)) throw new Error("passphrase plaintext mismatch");
  if (r1.filename !== g1.filename) throw new Error("filename mismatch: " + r1.filename);
  console.log("ok: v1_passphrase");

  // encrypt + decrypt round trip
  const enc = globalThis.deaddrop.encrypt(new TextEncoder().encode("wasm hello"), {
    contentType: "text/plain",
    filename: "",
  });
  if (enc.error) throw new Error(enc.error);
  const dec = globalThis.deaddrop.decrypt(enc.blob, enc.key, "");
  if (dec.error) throw new Error(dec.error);
  if (new TextDecoder().decode(dec.plaintext) !== "wasm hello") {
    throw new Error("round trip failed");
  }
  console.log("ok: encrypt/decrypt round trip");
  console.log("all wasm vector checks passed");
  process.exit(0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
