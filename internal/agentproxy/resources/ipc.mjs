import fs from "node:fs/promises";
import path from "node:path";
import { home, privateRead } from "./storage.mjs";
export async function connect() {
  const dir = path.join(home, ".copilot/run");
  const port = (await fs.readFile(path.join(dir, "ws.port"), "utf8")).split(
    "\n",
  )[0];
  const token = (await privateRead(path.join(dir, "ws.token"))).split("\n")[0];
  if (!/^\d+$/.test(port)) throw Error("Invalid Copilot port");
  const ws = new WebSocket(
    `ws://127.0.0.1:${port}/?token=${encodeURIComponent(token)}`,
  );
  let n = 0;
  const pending = new Map();
  await new Promise((res, rej) => {
    const timer = setTimeout(() => {
      ws.close();
      rej(Error("Copilot connection timeout"));
    }, 5000);
    ws.onopen = () => {
      clearTimeout(timer);
      res();
    };
    ws.onerror = () => {
      clearTimeout(timer);
      rej(Error("Copilot unavailable"));
    };
  });
  ws.onmessage = (e) => {
    try {
      const j = JSON.parse(e.data);
      const p = pending.get(j.request_id);
      if (p) {
        pending.delete(j.request_id);
        clearTimeout(p.timer);
        j.type === "error"
          ? p.reject(Error("Copilot rejected model operation"))
          : p.resolve(j);
      } else if (j.type === "error") {
        for (const p of pending.values()) {
          clearTimeout(p.timer);
          p.reject(Error("Copilot rejected operation"));
        }
        pending.clear();
      }
    } catch {}
  };
  return {
    close: () => ws.close(),
    call: (type, args = {}) =>
      new Promise((resolve, reject) => {
        const request_id = `model-sync-${++n}`;
        const timer = setTimeout(() => {
          pending.delete(request_id);
          reject(Error("Copilot operation timeout"));
        }, 10000);
        pending.set(request_id, { resolve, reject, timer });
        ws.send(JSON.stringify({ type, request_id, ...args }));
      }),
  };
}
