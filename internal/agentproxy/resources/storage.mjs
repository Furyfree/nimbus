import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
export class Failure extends Error {}
export const home = process.env.HOME;
function xdg(key, fallback) {
  const value = process.env[key] || path.join(home, fallback);
  if (!path.isAbsolute(value)) throw new Failure(`${key} must be absolute`);
  return value;
}
export const data = path.join(
  xdg("XDG_DATA_HOME", ".local/share"),
  "agent-proxy",
);
export const stateDir = path.join(
  xdg("XDG_STATE_HOME", ".local/state"),
  "nimbus",
);
export const configDir = path.join(
  xdg("XDG_CONFIG_HOME", ".config"),
  "agent-proxy",
);
export const statePath = path.join(stateDir, "agent-proxy.json");
export const legacyPath = path.join(
  xdg("XDG_STATE_HOME", ".local/state"),
  "agent-proxy/model-sync.json",
);
export async function ancestors(file) {
  for (let p = path.dirname(file); p !== path.dirname(p); p = path.dirname(p)) {
    try {
      const st = await fs.lstat(p);
      if (
        !st.isDirectory() ||
        st.isSymbolicLink() ||
        (st.mode & 0o022 && !(st.mode & 0o1000))
      )
        throw new Failure("Unsafe writable path ancestor");
    } catch (e) {
      if (e.code !== "ENOENT") throw e;
    }
  }
}
export async function privateRead(file) {
  await ancestors(file);
  const handle = await fs.open(
    file,
    fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW,
  );
  try {
    const st = await handle.stat();
    if (
      !st.isFile() ||
      st.uid !== process.getuid() ||
      st.nlink !== 1 ||
      st.mode & 0o077 ||
      st.size > 4 * 1024 * 1024
    )
      throw new Failure("Unsafe private file permissions or size");
    return await handle.readFile("utf8");
  } finally {
    await handle.close();
  }
}
export async function atomic(file, value) {
  await ancestors(file);
  try {
    await privateRead(file);
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
  const tmp = file + "." + randomUUID() + ".tmp";
  try {
    const handle = await fs.open(tmp, "wx", 0o600);
    try {
      await handle.writeFile(JSON.stringify(value, null, 2) + "\n");
      await handle.sync();
    } finally {
      await handle.close();
    }
    await fs.rename(tmp, file);
  } finally {
    await fs.unlink(tmp).catch((e) => {
      if (e.code !== "ENOENT") throw e;
    });
  }
}
export function validateState(s, machine) {
  const allowed = new Set([
    "version",
    "machine",
    "providerId",
    "configured",
    "entries",
    "catalogs",
    "lastSuccess",
    "discovery",
    "importedTrial",
  ]);
  if (!s || Object.keys(s).some((key) => !allowed.has(key)))
    throw new Failure("Unrecognized model state fields");
  if (
    s.version !== 1 ||
    s.machine !== machine ||
    typeof s.providerId !== "string" ||
    !s.providerId ||
    typeof s.configured !== "boolean" ||
    !Array.isArray(s.entries)
  )
    throw new Failure("Invalid or foreign-machine model ownership state");
  const aliases = new Set(),
    keys = new Set(),
    ids = new Set();
  for (const e of s.entries) {
    if (
      !["codex", "claude", "grok", "agy"].includes(e.provider) ||
      typeof e.key !== "string" ||
      !e.key ||
      typeof e.alias !== "string" ||
      !e.alias ||
      e.alias.length > 512 ||
      /[\x00-\x1f\x7f]/.test(e.alias) ||
      aliases.has(e.alias) ||
      keys.has(e.provider + "\0" + e.key)
    )
      throw new Failure("Invalid model ownership entry");
    aliases.add(e.alias);
    keys.add(e.provider + "\0" + e.key);
    for (const k of ["proxyId", "copilotId"])
      if (e[k]) {
        if (typeof e[k] !== "string" || ids.has(k + e[k]))
          throw new Failure("Invalid owned model ID");
        ids.add(k + e[k]);
      }
  }
  return s;
}
