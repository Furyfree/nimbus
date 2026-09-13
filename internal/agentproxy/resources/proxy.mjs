import { Failure, configDir, privateRead } from "./storage.mjs";
import path from "node:path";
export async function localAPI() {
  const env = Object.fromEntries(
    (await privateRead(path.join(configDir, "agent-proxy.env")))
      .split("\n")
      .filter((x) => x.includes("=") && !x.startsWith("#"))
      .map((x) => {
        const i = x.indexOf("=");
        return [x.slice(0, i), x.slice(i + 1).replace(/^"|"$/g, "")];
      }),
  );
  if (!env.ADMIN_TOKEN || !env.PROXY_API_KEY)
    throw new Failure("Native installer credentials missing");
  const api = async (route, method = "GET", body) => {
    let r;
    try {
      r = await fetch("http://127.0.0.1:8300" + route, {
        method,
        headers: {
          Authorization: `Bearer ${env.ADMIN_TOKEN}`,
          ...(body ? { "Content-Type": "application/json" } : {}),
        },
        ...(body ? { body: JSON.stringify(body) } : {}),
        signal: AbortSignal.timeout(10000),
      });
    } catch {
      throw new Failure("Local proxy is unavailable; inspect its user service");
    }
    if (!r.ok) throw new Failure(`Proxy operation failed: HTTP ${r.status}`);
    return r.status === 204 ? null : r.json();
  };
  return { env, api };
}
export async function ready(api) {
  for (let i = 0; i < 40; i++) {
    try {
      await api("/admin/model-mappings");
      return;
    } catch {}
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Failure("Proxy did not become ready; inspect its user service");
}
export function validateConfig(config) {
  if (
    config.server?.host !== "127.0.0.1" ||
    config.server?.port !== 8300 ||
    config.auth?.enabled !== true ||
    config.auth?.admin_token !== "${ADMIN_TOKEN}" ||
    !Array.isArray(config.model_mappings) ||
    config.model_mappings.length
  )
    throw new Failure(
      "Expected loopback, authenticated configuration with no static model seeds",
    );
  for (const p of ["codex", "claude", "grok", "agy"])
    if (config.providers?.[p]?.enabled !== true)
      throw new Failure(`Managed ${p} backend configuration is missing`);
}
