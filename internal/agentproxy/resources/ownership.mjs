// Shared read-only ownership inspection for preview and approved removal.
import fs from "node:fs/promises";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { data, configDir, ancestors, Failure } from "./storage.mjs";

const exec = promisify(execFile);
export const units = ["agent-proxy.service", "herdr.service"];
export async function stat(file) {
  await ancestors(file);
  try {
    const info = await fs.lstat(file);
    if (info.isSymbolicLink() || info.uid !== process.getuid() || (info.mode & 0o022))
      throw new Failure(`Unrecognized proxy ownership: ${file}; refusing removal`);
    return info;
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
}
export async function unitState(unit) {
  const { stdout } = await exec("systemctl", ["--user", "show", unit,
    "--property=LoadState,ActiveState,FragmentPath,DropInPaths"]);
  const fields = Object.fromEntries(stdout.trim().split("\n").map((s) => {
    const i = s.indexOf("=");
    return [s.slice(0, i), s.slice(i + 1)];
  }));
  if (!fields.LoadState || !fields.ActiveState)
    throw new Failure(`Cannot verify proxy user service: ${unit}`);
  return fields;
}
export async function checkUninstall(release) {
  const installed = await stat(data);
  if (installed) {
    if (!installed.isDirectory()) throw new Failure(`Invalid proxy installation directory: ${data}`);
    const current = await fs.realpath(path.join(data, "current"));
    if (current !== path.join(data, "releases", release))
      throw new Failure(`Unrecognized proxy release: ${data}/current; refusing removal`);
    await ancestors(path.join(current, "VERSION"));
    if ((await fs.readFile(path.join(current, "VERSION"), "utf8")).trim() !== release)
      throw new Failure(`Unrecognized proxy release: ${data}/current; refusing removal`);
  }
  for (const unit of units) {
    const file = path.join(path.dirname(configDir), "systemd/user", unit);
    const info = await stat(file);
    if (info) {
      const entry = unit === "herdr.service" ? "herdr/server.js" : "index.js";
      if (!info.isFile() || !(await fs.readFile(file, "utf8")).split("\n").some(
        (line) => line === `ExecStart=/usr/bin/env node "${data}/current/packages/server/dist/${entry}"`,
      )) throw new Failure(`Unrecognized proxy user service ExecStart: ${file}; refusing removal`);
    }
    const observed = await unitState(unit);
    if (observed.LoadState !== "not-found" && (!info || observed.FragmentPath !== file))
      throw new Failure(`Foreign proxy service: ${unit} loaded from ${observed.FragmentPath}; refusing removal`);
    await checkDropIns(observed.DropInPaths);
  }
}

// Accept only Fedora's shared timeout policy, never arbitrary service overrides.
export async function checkDropIns(dropIns) {
  for (const file of (dropIns || "").split(/\s+/).filter(Boolean)) {
    if (file !== "/usr/lib/systemd/user/service.d/10-timeout-abort.conf")
      throw new Failure(`Unrecognized proxy service override: ${file}`);
    let info, contents;
    try {
      info = (await exec("stat", ["--format=%u:%a:%F", "--", file],
        { env: { ...process.env, LC_ALL: "C" } })).stdout.trim();
    } catch {
      throw new Failure(`Cannot inspect Fedora timeout override: ${file}`);
    }
    if (info !== "0:644:regular file")
      throw new Failure(`Unsafe Fedora timeout override ownership or permissions: ${file}`);
    try { contents = (await exec("cat", ["--", file])).stdout; }
    catch { throw new Failure(`Cannot read Fedora timeout override: ${file}`); }
    const settings = contents.split("\n").map(line => line.trim())
      .filter(line => line && !line.startsWith("#") && !line.startsWith(";"));
    if (settings.join("\n") !== "[Service]\nTimeoutStopFailureMode=abort")
      throw new Failure(`Unexpected settings in Fedora timeout override: ${file}`);
  }
}
