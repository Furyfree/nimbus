// Native installer from upstream 66cc75af597f; no --purge, so user data stays.
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { data, configDir, statePath, ancestors, Failure } from "./storage.mjs";
import { installedRelease, release, command } from "./setup.mjs";
import { connect } from "./ipc.mjs";

const exec = promisify(execFile);
const units = ["agent-proxy.service", "herdr.service"];
async function stat(file) {
  await ancestors(file);
  try {
    const info = await fs.lstat(file);
    if (info.isSymbolicLink() || info.uid !== process.getuid())
      throw new Failure("Unrecognized proxy ownership; refusing removal");
    return info;
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
}
async function unitState(unit) {
  const { stdout } = await exec("systemctl", ["--user", "show", unit,
    "--property=LoadState,ActiveState,FragmentPath,DropInPaths"]);
  const fields = Object.fromEntries(stdout.trim().split("\n").map((s) => {
    const i = s.indexOf("=");
    return [s.slice(0, i), s.slice(i + 1)];
  }));
  if (!fields.LoadState || !fields.ActiveState)
    throw new Failure("Cannot verify proxy user services");
  return fields;
}
export async function uninstall(state, report, open = connect) {
  const installed = await stat(data);
  if (installed) {
    if (!installed.isDirectory()) throw new Failure("Invalid proxy installation directory");
    const current = await fs.realpath(path.join(data, "current"));
    if (current !== path.join(data, "releases", release))
      throw new Failure("Unrecognized proxy release; refusing removal");
    await ancestors(path.join(current, "VERSION"));
    if ((await installedRelease()) !== release)
      throw new Failure("Unrecognized proxy release; refusing removal");
  }
  for (const unit of units) {
    const file = path.join(path.dirname(configDir), "systemd/user", unit);
    const info = await stat(file);
    if (info) {
      const entry = unit === "herdr.service" ? "herdr/server.js" : "index.js";
      if (!info.isFile() || !(await fs.readFile(file, "utf8")).split("\n").some(
        (line) => line === `ExecStart=/usr/bin/env node "${data}/current/packages/server/dist/${entry}"`,
      )) throw new Failure("Unrecognized proxy user service; refusing removal");
    }
    const observed = await unitState(unit);
    if (observed.DropInPaths || (observed.LoadState !== "not-found" && (!info || observed.FragmentPath !== file)))
      throw new Failure("Foreign or overridden proxy service; refusing removal");
  }
  let rpc;
  if (state) {
    try { rpc = await open(); } catch {
      report.notices.push("Copilot is unavailable; remove its stale Local agents provider in the app.");
    }
  }
  try {
    if (rpc) {
      const { providers } = await rpc.call("list_model_providers");
      if (!Array.isArray(providers)) throw new Failure("Incomplete Copilot provider inventory");
      const provider = providers.find((p) => p.id === state.providerId);
      if (provider) {
        if (provider.kind !== "openai" || provider.settings?.baseUrl !== "http://127.0.0.1:8300/v1" || provider.settings?.wireApi !== "completions")
          throw new Failure("Registered provider settings changed; refusing removal");
        await rpc.call("delete_model_provider", { provider_id: state.providerId });
        const after = (await rpc.call("list_model_providers")).providers;
        if (!Array.isArray(after) || after.some((p) => p.id === state.providerId))
          throw new Failure("Copilot provider removal was not verified");
        report.changes.push("Unregistered the owned Copilot provider");
      }
    }
  } finally { rpc?.close(); }
  // Upstream tolerates stop failures. Require stopped services before it deletes files.
  for (const unit of units) {
    if ((await unitState(unit)).LoadState !== "not-found") {
      await command("systemctl", ["--user", "disable", "--now", unit]);
      const observed = await unitState(unit);
      if (!["inactive", "failed"].includes(observed.ActiveState))
        throw new Failure("Proxy service did not stop; installation retained");
    }
  }
  await command("bash", [fileURLToPath(new URL("upstream-install.sh", import.meta.url)), "uninstall"]);
  for (const unit of units) {
    const observed = await unitState(unit);
    if (observed.LoadState !== "not-found" || !["inactive", "failed"].includes(observed.ActiveState))
      throw new Failure("Proxy service removal was not verified; registration retained for retry");
  }
  if (await stat(data)) throw new Failure("Proxy installation removal was not verified");
  await fs.rm(statePath, { force: true });
  report.changes.push("Removed proxy services, installation and Nimbus registration");
  report.status = "succeeded";
  report.detail = "Agent proxy uninstalled. Copilot, config.yaml and Mise Herdr retained.";
}
