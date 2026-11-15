importScripts("wasm_exec.js");

const go = new Go();
let wasmReady = false;

async function initWasm() {
  try {
    const result = await WebAssembly.instantiateStreaming(
      fetch("main.wasm"),
      go.importObject
    );
    go.run(result.instance);
    wasmReady = Boolean(self.xenonReady && typeof self.upscaleImage === "function");
    if (!wasmReady) {
      self.postMessage({ type: "error", error: "WASM runtime failed to initialize" });
      return;
    }
    self.postMessage({ type: "ready" });
  } catch (err) {
    self.postMessage({
      type: "error",
      error: err && err.message ? err.message : String(err),
    });
  }
}

initWasm();

self.onmessage = (event) => {
  const msg = event.data;
  if (!msg || msg.type !== "upscale") return;

  if (!wasmReady) {
    self.postMessage({
      type: "result",
      id: msg.id,
      error: "WASM is not ready yet",
    });
    return;
  }

  try {
    const res = self.upscaleImage(
      msg.bytes,
      msg.scale,
      msg.algorithm,
      msg.sharpen,
      msg.format,
      msg.quality
    );

    if (res.error) {
      self.postMessage({
        type: "result",
        id: msg.id,
        error: res.error,
      });
      return;
    }

    const data = res.data;
    self.postMessage(
      {
        type: "result",
        id: msg.id,
        data,
        width: res.width,
        height: res.height,
        origWidth: res.origWidth,
        origHeight: res.origHeight,
        format: res.format,
        durationMs: res.durationMs,
      },
      [data.buffer]
    );
  } catch (err) {
    self.postMessage({
      type: "result",
      id: msg.id,
      error: err && err.message ? err.message : String(err),
    });
  }
};
