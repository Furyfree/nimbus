import { localAPI, ready, validateConfig } from "./proxy.mjs";
import { Failure } from "./storage.mjs";
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { discover } from "./discovery.mjs";
import { connect } from "./ipc.mjs";
import { reconcile, checkOwnership } from "./catalog.mjs";
import {
  home,
  data,
  configDir,
  statePath,
  legacyPath,
  ancestors,
  privateRead,
  atomic,
  validateState,
} from "./storage.mjs";
import { install, installedRelease, release, command } from "./setup.mjs";
import { uninstall } from "./uninstall.mjs";
const report = {
  status: "failed",
  detail: "Agent-proxy setup did not finish.",
  changes: [],
  notices: [],
  providers: [],
};
let stage = "local registration inspection";
function providerMatches(p) {
  return (
    p?.kind === "openai" &&
    p.settings?.baseUrl === "http://127.0.0.1:8300/v1" &&
    p.settings?.wireApi === "completions"
  );
}
async function loadState(machine, setup) {
  try {
    return validateState(JSON.parse(await privateRead(statePath)), machine);
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
  if (!setup) return null;
  // Explicit local-trial handoff. No model is adopted merely by its name.
  try {
    const old = JSON.parse(await privateRead(legacyPath));
    const s = validateState({ ...old, machine, configured: false }, machine);
    s.importedTrial = true;
    return s;
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
  return {
    version: 1,
    machine,
    configured: false,
    providerId: randomUUID(),
    entries: [],
  };
}
async function main() {
  const args = process.argv.slice(2),
    setup = args[0] === "--setup";
  if (
    args.length !== 3 ||
    !["--setup", "--refresh", "--disable-refresh", "--uninstall"].includes(args[0]) ||
    args[1] !== "--machine" ||
    !args[2]
  )
    throw new Failure("Invalid adapter arguments");
  const machine = args[2],
    state = await loadState(machine, setup);
  if (args[0] === "--uninstall") {
    stage = "proxy uninstall";
    await uninstall(state, report);
    return;
  }
  if (!state || (!setup && !state.configured)) {
    report.status = "skipped";
    report.detail = "Agent proxy is not configured; no model refresh.";
    return;
  }
  if (args[0] === "--disable-refresh") {
    state.configured = false;
    await atomic(statePath, state);
    report.status = "succeeded";
    report.detail =
      "Automatic model refresh disabled; configuration, services and models retained.";
    return;
  }
  let rpc;
  try {
    rpc = await connect();
  } catch {
    report.status = "deferred";
    report.detail =
      "Open GitHub Copilot, then run nimbus postinstall agent-proxy. Existing models preserved.";
    return;
  }
  try {
    const providers = (await rpc.call("list_model_providers")).providers;
    if (!Array.isArray(providers))
      throw new Failure("Copilot returned an incomplete provider inventory");
    const provider = providers.find((p) => p.id === state.providerId);
    if (provider && !providerMatches(provider))
      throw new Failure(
        "Registered provider settings changed; refusing to overwrite them",
      );
    if (!provider && (state.configured || state.importedTrial))
      throw new Failure(
        "Registered provider is missing; preserve state and inspect Copilot",
      );
    let installed = false,
      configChanged = false;
    if (setup) {
      if (await installedRelease()) {
        const { api } = await localAPI();
        await ready(api);
        const active = await api("/admin/active-requests");
        if (active.count !== 0)
          throw new Failure(
            "Proxy has active requests; retry after they finish",
          );
      }
      let before = "";
      try {
        before = await fs.readFile(path.join(configDir, "config.yaml"), "utf8");
      } catch (e) {
        if (e.code !== "ENOENT") throw e;
      }
      stage = "configuration and native installation";
      process.stderr.write(
        "Apply only the managed agent-proxy configuration. Other dotfiles and scripts are excluded.\n",
      );
      await command("chezmoi", [
        "--skip-secrets",
        "apply",
        "--exclude=scripts",
        path.join(configDir, "config.yaml"),
      ]);
      const workspace = path.join(path.dirname(legacyPath), "workspace");
      await ancestors(path.join(workspace, "check"));
      await fs.mkdir(workspace, { recursive: true, mode: 0o700 });
      configChanged =
        before !==
        (await fs.readFile(path.join(configDir, "config.yaml"), "utf8"));
      installed = await install();
      if (installed)
        report.changes.push(`Installed ${release} through native installer`);
    } else if ((await installedRelease()) !== release) {
      report.status = "deferred";
      report.detail =
        "Proxy installation needs setup; run nimbus postinstall agent-proxy.";
      return;
    }
    // Static configuration and native-generated credentials must keep the API local.
    stage = "proxy configuration validation";
    const require = createRequire(path.join(data, "current/package.json"));
    const YAML = require("yaml");
    const config = YAML.parse(
      await fs.readFile(path.join(configDir, "config.yaml"), "utf8"),
    );
    validateConfig(config);
    const { env, api } = await localAPI();
    await ready(api);
    if (setup && configChanged && !installed) {
      const active = await api("/admin/active-requests");
      if (active.count !== 0)
        throw new Failure("Proxy has active requests; retry after they finish");
      await command("systemctl", ["--user", "restart", "agent-proxy.service"]);
      await ready(api);
    }
    stage = "native ownership inspection";
    if (state.importedTrial) {
      const maps = await api("/admin/model-mappings");
      const models = (
        await rpc.call("list_provider_models", {
          provider_id: state.providerId,
        })
      ).models;
      checkOwnership(state, maps, models);
      if (
        state.entries.some(
          (e) =>
            !maps.some((m) => m.id === e.proxyId) ||
            !models.some((m) => m.id === e.copilotId),
        )
      )
        throw new Failure("Trial ownership does not match native inventory");
    }
    await atomic(statePath, state);
    if (setup) {
      const r = await rpc.call("upsert_model_provider", {
        provider: provider ?? {
          id: state.providerId,
          name: "Local agents",
          kind: "openai",
          settings: {
            baseUrl: "http://127.0.0.1:8300/v1",
            wireApi: "completions",
            authKind: "api_key",
            headersJson: "{}",
          },
        },
        secret: { kind: "api_key", value: env.PROXY_API_KEY },
      });
      if (r.provider?.id !== state.providerId)
        throw new Failure("Copilot did not confirm provider registration");
    }
    stage = "Copilot provider connection test";
    const test = await rpc.call("test_model_provider", {
      provider_id: state.providerId,
    });
    if (!test.ok)
      throw new Failure("Copilot could not verify the local provider");
    stage = "model discovery";
    const catalogs = await discover(report.providers);
    if (
      !Object.keys(catalogs).length &&
      report.providers.every((p) => p.status === "skipped")
    ) {
      report.status = "deferred";
      report.detail = "No model lists available. Existing entries preserved.";
      return;
    }
    if (!Object.keys(catalogs).length)
      throw new Failure(
        "No provider catalog could be checked; existing entries preserved",
      );
    stage = "model reconciliation";
    await reconcile(
      state,
      catalogs,
      api,
      rpc,
      (s) => atomic(statePath, s),
      report.changes,
    );

    state.configured = true;
    delete state.importedTrial;
    await atomic(statePath, state);
    for (const provider of report.providers)
      if (provider.status === "discovered") provider.status = "verified";
    const count = Object.values(catalogs).reduce(
      (sum, models) => sum + models.length,
      0,
    );
    report.status = report.providers.some((p) => p.status === "failed")
      ? "warning"
      : "succeeded";
    report.detail = `${count} models up to date. ${report.changes.length ? `${report.changes.length} changes made.` : "No changes needed."}`;
  } finally {
    rpc.close();
  }
}
await main().catch((e) => {
  report.status = "failed";
  report.detail =
    e instanceof Failure
      ? e.message
      : `Agent-proxy operation failed during ${stage}; ownership evidence retained for retry.`;
  process.exitCode = 1;
});
process.stdout.write(JSON.stringify(report) + "\n");
