import fs from "node:fs/promises";
import path from "node:path";
import { spawn, execFile } from "node:child_process";
import { promisify } from "node:util";
import { stateDir } from "./storage.mjs";
const exec = promisify(execFile);
class NotSignedIn extends Error {}
export function validateCatalog(name, rows, allowEmpty = false) {
  if (!Array.isArray(rows) || (!allowEmpty && !rows.length))
    throw Error(
      `${name}: empty/incomplete catalog; preserving existing entries`,
    );
  const seen = new Set();
  for (const r of rows) {
    if (
      !r ||
      typeof r.key !== "string" ||
      !r.key ||
      typeof r.actual !== "string" ||
      !r.actual ||
      typeof r.name !== "string" ||
      !r.name ||
      [r.key, r.actual, r.name].some(
        (value) => value.length > 512 || /[\x00-\x1f\x7f]/.test(value),
      ) ||
      seen.has(r.key)
    )
      throw Error(`${name}: invalid or duplicate catalog entry`);
    seen.add(r.key);
  }
  return rows;
}
export function parseAgyModels(stdout, stderr = "") {
  const clean = (s) => s.replace(/\x1b\[[0-9;]*[A-Za-z]/g, "").trim();
  const diagnostics = clean(stderr)
    .split("\n")
    .filter((s) => s.trim() && s.trim() !== "Fetching available models...");
  if (diagnostics.length)
    throw Error(
      "Agy reported a discovery diagnostic; existing entries preserved",
    );
  const text = clean(stdout);
  if (/^No models available\.?$/i.test(text)) return [];
  const lines = text
    .split("\n")
    .map((s) => s.trim())
    .filter(
      (s) =>
        s && s !== "Fetching available models..." && s !== "Available models:",
    );
  if (!lines.length) throw Error("Agy returned no complete catalog");
  const rows = lines.map((line) => {
    const m = line.match(/^([a-z0-9][a-z0-9._-]*)\s+(.+)$/);
    if (!m)
      throw Error(
        "Agy model-list format is unrecognized; existing entries preserved",
      );
    return { key: m[1], actual: m[1], name: m[2].trim() };
  });
  return validateCatalog("agy", rows);
}
async function protocol(cmd, args, work) {
  const child = spawn(cmd, args, {
    cwd: stateDir,
    env: { ...process.env, CI: "1", NO_COLOR: "1" },
    stdio: ["pipe", "pipe", "ignore"],
  });
  let buffer = "",
    closed = false;
  const pending = new Map();
  child.stdout.on("data", (chunk) => {
    buffer += chunk;
    if (buffer.length > 4 * 1024 * 1024) {
      fail();
      child.kill();
      return;
    }
    let i;
    while ((i = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, i);
      buffer = buffer.slice(i + 1);
      try {
        const j = JSON.parse(line);
        const id = j.id ?? j.response?.request_id;
        const p = pending.get(id);
        if (p) {
          pending.delete(id);
          if (j.error || j.response?.subtype === "error")
            p.reject(Error(`${cmd}: model discovery rejected`));
          else p.resolve(j.result ?? j.response?.response);
        }
      } catch {}
    }
  });
  const fail = () => {
    closed = true;
    for (const p of pending.values())
      p.reject(Error(`${cmd}: discovery process ended`));
    pending.clear();
  };
  child.on("error", fail);
  child.on("exit", fail);
  const call = (id, method, params) =>
    new Promise((resolve, reject) => {
      if (closed) return reject(Error(`${cmd}: discovery unavailable`));
      pending.set(id, { resolve, reject });
      const msg =
        cmd === "claude"
          ? {
              type: "control_request",
              request_id: id,
              request: { subtype: method, ...params },
            }
          : { id, method, params };
      child.stdin.write(JSON.stringify(msg) + "\n");
    });
  child.stdin.on("error", fail);
  let timer;
  try {
    return await Promise.race([
      work(call, (j) => child.stdin.write(JSON.stringify(j) + "\n")),
      new Promise((_, reject) => {
        timer = setTimeout(
          () => reject(Error(`${cmd}: discovery timed out`)),
          30000,
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
    child.kill("SIGTERM");
    const kill = setTimeout(() => child.kill("SIGKILL"), 2000);
    kill.unref();
  }
}
async function discoverOne(provider) {
  if (provider === "codex") {
    await exec("codex", ["login", "status"], { timeout: 15000 });
    return await protocol(
      "codex",
      ["app-server", "--stdio", "-c", 'model_provider="openai"'],
      async (call, notify) => {
        await call(1, "initialize", {
          clientInfo: { name: "local_model_sync", version: "0.1" },
        });
        notify({ method: "initialized" });
        let cursor = null,
          id = 2,
          rows = [],
          seen = new Set();
        do {
          const j = await call(id++, "model/list", {
            includeHidden: false,
            limit: 100,
            cursor,
          });
          if (!Array.isArray(j?.data) || !Object.hasOwn(j, "nextCursor"))
            throw Error("Codex returned an incomplete page");
          rows.push(
            ...j.data
              .filter((m) => !m.hidden)
              .map((m) => ({
                key: m.model,
                actual: m.model,
                name: m.displayName,
              })),
          );
          if (
            j.nextCursor !== null &&
            (typeof j.nextCursor !== "string" || !j.nextCursor)
          )
            throw Error("Codex returned an invalid cursor");
          cursor = j.nextCursor;
          if (cursor) {
            if (seen.has(cursor)) throw Error("Codex pagination repeated");
            seen.add(cursor);
          }
        } while (cursor);
        return validateCatalog("codex", rows, true);
      },
    );
  }
  if (provider === "claude") {
    const auth = JSON.parse(
      (await exec("claude", ["auth", "status", "--json"], { timeout: 15000 }))
        .stdout,
    );
    if (!auth.loggedIn) throw new NotSignedIn();
    return await protocol(
      "claude",
      [
        "-p",
        "--input-format",
        "stream-json",
        "--output-format",
        "stream-json",
        "--verbose",
        "--no-session-persistence",
        "--setting-sources",
        "",
        "--strict-mcp-config",
        "--mcp-config",
        '{"mcpServers":{}}',
        "--tools",
        "",
      ],
      async (call) => {
        const j = await call("catalog", "initialize", {});
        return validateCatalog(
          "claude",
          j?.models?.map((m) => ({
            key: m.value,
            actual: m.value === "default" ? m.resolvedModel : m.value,
            name: m.displayName,
            resolved: m.resolvedModel,
          })),
          true,
        );
      },
    );
  }
  if (provider === "grok") {
    const { stdout } = await exec("grok", ["models"], {
      timeout: 30000,
      maxBuffer: 1024 * 1024,
      env: { ...process.env, NO_COLOR: "1", TERM: "dumb" },
    });
    if (
      !stdout.includes("You are logged in") ||
      !stdout.includes("Available models:")
    )
      throw Error("Grok did not return an authenticated model list");
    const lines = stdout
      .split("Available models:")[1]
      .trim()
      .split("\n")
      .filter((x) => x.trim());
    return validateCatalog(
      "grok",
      lines.map((line) => {
        const m = line.match(/^\s*[*-]\s+(\S+?)(?:\s+\(default\))?\s*$/);
        if (!m) throw Error("Grok model list format changed");
        return { key: m[1], actual: m[1], name: m[1] };
      }),
    );
  }
  const { stdout, stderr } = await exec("agy", ["models"], {
    timeout: 30000,
    maxBuffer: 1024 * 1024,
    env: {
      ...process.env,
      NO_COLOR: "1",
      TERM: "dumb",
      AGY_CLI_HIDE_ACCOUNT_INFO: "1",
    },
  });
  return parseAgyModels(stdout, stderr);
}
export async function discover(results, inspect = discoverOne) {
  const catalogs = {};
  for (const provider of ["codex", "claude", "grok", "agy"]) {
    try {
      catalogs[provider] = await inspect(provider);
      results.push({
        provider,
        status: "discovered",
        count: catalogs[provider].length,
      });
    } catch (error) {
      // Inspect only known diagnostics; never print account output or raw errors.
      const auth =
        error instanceof NotSignedIn ||
        (provider === "agy" &&
          /Please sign in to view available models/.test(error.stderr ?? "")) ||
        (provider === "codex" &&
          /not logged in/i.test((error.stderr ?? "") + (error.stdout ?? "")));
      const missing = error.code === "ENOENT";
      results.push({
        provider,
        status: auth || missing ? "skipped" : "failed",
        count: 0,
        reason: auth
          ? "not signed in"
          : missing
            ? "CLI not installed"
            : "could not read model list; existing entries preserved",
      });
    }
  }
  return catalogs;
}
