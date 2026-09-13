import { Failure } from "./storage.mjs";
import { randomUUID } from "node:crypto";
import { validateCatalog } from "./discovery.mjs";
const labels = {
  codex: "Codex",
  claude: "Claude",
  grok: "Grok",
  agy: "Antigravity",
};
export function staleEntries(entries, catalogs) {
  for (const [p, rows] of Object.entries(catalogs))
    validateCatalog(p, rows, true);
  return entries.filter(
    (e) =>
      Object.hasOwn(catalogs, e.provider) &&
      !catalogs[e.provider].some((m) => m.key === e.key),
  );
}
// A journal intent permits recovery of a successful POST whose response was lost.
// It must match all submitted fields, never just an alias belonging to a user.
export function checkOwnership(state, mappings, models) {
  if (!Array.isArray(mappings) || !Array.isArray(models))
    throw new Failure("Incomplete destination inventory");
  for (const e of state.entries) {
    const mp = mappings.filter((m) => m.alias === e.alias),
      cm = models.filter((m) => m.modelId === e.alias);
    if (
      !e.proxyId &&
      e.creating &&
      mp.length === 1 &&
      mp[0].provider === e.provider &&
      mp[0].actualModel === e.creating.actual &&
      mp[0].displayName === e.creating.display
    )
      e.proxyId = mp[0].id;
    if (
      mp.some((m) => m.id !== e.proxyId) ||
      cm.some((m) => m.id !== e.copilotId)
    )
      throw new Failure(`Unmanaged collision: ${e.alias}`);
    const m = mappings.find((m) => m.id === e.proxyId),
      c = models.find((m) => m.id === e.copilotId);
    if (
      (m && (m.alias !== e.alias || m.provider !== e.provider)) ||
      (c && (c.modelId !== e.alias || c.providerId !== state.providerId))
    )
      throw new Failure(`Owned model identity changed: ${e.alias}`);
  }
}
export async function reconcile(state, catalogs, api, rpc, save, changes = []) {
  const pid = state.providerId;
  const mappings = await api("/admin/model-mappings");
  const models = (await rpc.call("list_provider_models", { provider_id: pid }))
    .models;
  checkOwnership(state, mappings, models);
  const stale = staleEntries(state.entries, catalogs),
    desired = [];
  for (const [provider, rows] of Object.entries(catalogs))
    for (const m of rows) {
      let e = state.entries.find(
        (e) => e.provider === provider && e.key === m.key,
      );
      if (!e) {
        e = {
          provider,
          key: m.key,
          alias: `${provider}--${encodeURIComponent(m.key)}`,
          copilotId: randomUUID(),
        };
        if (
          mappings.some((x) => x.alias === e.alias) ||
          models.some((x) => x.modelId === e.alias)
        )
          throw new Failure(`Unmanaged collision: ${e.alias}`);
      }
      desired.push({ e, m });
    }
  if (stale.length)
    process.stderr.write(
      `Removing ${stale.length} stale managed model entries...\n`,
    );

  for (const { e, m } of desired) {
    const display = `${labels[e.provider]} · ${m.name}`;
    if (!state.entries.includes(e)) state.entries.push(e);
    const pm = mappings.find((x) => x.id === e.proxyId);
    if (!pm) {
      e.creating = { actual: m.actual, display };
      await save(state);
      const created = await api("/admin/model-mappings", "POST", {
        alias: e.alias,
        provider: e.provider,
        actual_model: m.actual,
        display_name: display,
      });
      if (!created?.id) throw new Failure("Proxy did not confirm creation");
      e.proxyId = created.id;
      delete e.creating;
      await save(state);
      changes.push(`Added ${e.alias} to proxy`);
    } else if (
      pm.actualModel !== m.actual ||
      pm.displayName !== display ||
      !pm.enabled
    ) {
      await api("/admin/model-mappings/" + encodeURIComponent(pm.id), "PUT", {
        actual_model: m.actual,
        display_name: display,
        enabled: true,
      });
      changes.push(`Updated ${e.alias} in proxy`);
    }
    const cm = models.find((x) => x.id === e.copilotId);
    if (!cm || cm.displayName !== display) {
      e.copilotId ??= randomUUID();
      await save(state);
      const result = await rpc.call("upsert_provider_model", {
        model: {
          ...(cm ?? {
            id: e.copilotId,
            providerId: pid,
            modelId: e.alias,
            maxPromptTokens: 64000,
            maxOutputTokens: 8192,
          }),
          displayName: display,
        },
      });
      if (result.model?.id !== e.copilotId)
        throw new Failure("Copilot did not confirm expected model");
      changes.push(`Updated ${e.alias} in Copilot`);
    }
    await save(state);
  }
  for (const e of stale) {
    // Recheck immediately before deleting. Changed identity blocks deletion.
    const current = await api("/admin/model-mappings");
    const selected = (
      await rpc.call("list_provider_models", { provider_id: pid })
    ).models;
    checkOwnership(state, current, selected);
    if (selected.some((m) => m.id === e.copilotId))
      await rpc.call("delete_provider_model", { model_id: e.copilotId });
    if (current.some((m) => m.id === e.proxyId))
      await api(
        "/admin/model-mappings/" + encodeURIComponent(e.proxyId),
        "DELETE",
      );
    const after = await api("/admin/model-mappings");
    const cm = (await rpc.call("list_provider_models", { provider_id: pid }))
      .models;
    if (
      after.some((m) => m.id === e.proxyId) ||
      cm.some((m) => m.id === e.copilotId)
    )
      throw new Failure("Removal was not verified");
    state.entries = state.entries.filter((x) => x !== e);
    await save(state);
    changes.push(`Removed ${e.alias}`);
  }
  const finalProxy = await api("/admin/model-mappings");
  const finalModels = (
    await rpc.call("list_provider_models", { provider_id: pid })
  ).models;
  checkOwnership(state, finalProxy, finalModels);
  for (const { e, m } of desired) {
    const pm = finalProxy.find((x) => x.id === e.proxyId),
      cm = finalModels.find((x) => x.id === e.copilotId);
    if (
      !pm?.enabled ||
      pm.actualModel !== m.actual ||
      !cm ||
      cm.modelId !== e.alias
    )
      throw new Failure("Final registration verification failed");
  }
  state.catalogs = { ...state.catalogs, ...catalogs };
  state.lastSuccess = new Date().toISOString();
  await save(state);
  return changes;
}
