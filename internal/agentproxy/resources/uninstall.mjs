// Native installer from upstream 66cc75af597f; no --purge, so user data stays.
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { data, statePath, Failure } from "./storage.mjs";
import { release, command } from "./setup.mjs";
import { checkUninstall, stat, unitState, units } from "./ownership.mjs";
import { connect } from "./ipc.mjs";

export async function uninstall(state, report, open = connect) {
  await checkUninstall(release);
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
