import test from "node:test";
import assert from "node:assert/strict";
import { reconcile, staleEntries, checkOwnership } from "./catalog.mjs";
import { parseAgyModels, validateCatalog } from "./discovery.mjs";
function fixture() {
  let maps = [],
    models = [],
    n = 0;
  const state = {
    version: 1,
    machine: "test",
    configured: true,
    providerId: "provider",
    entries: [],
  };
  let saved;
  const api = async (route, method = "GET", body) => {
    if (method === "GET") return structuredClone(maps);
    if (method === "POST") {
      const m = {
        id: "m" + ++n,
        alias: body.alias,
        provider: body.provider,
        actualModel: body.actual_model,
        displayName: body.display_name,
        enabled: true,
      };
      maps.push(m);
      return m;
    }
    const id = route.split("/").at(-1);
    if (method === "DELETE") {
      maps = maps.filter((m) => m.id !== id);
      return;
    }
    const m = maps.find((m) => m.id === id);
    m.actualModel = body.actual_model;
    m.displayName = body.display_name;
    m.enabled = body.enabled;
    return m;
  };
  const rpc = {
    call: async (type, args) => {
      if (type === "list_provider_models")
        return { models: structuredClone(models) };
      if (type === "upsert_provider_model") {
        models = models.filter((m) => m.id !== args.model.id);
        models.push(args.model);
        return { model: args.model };
      }
      if (type === "delete_provider_model") {
        models = models.filter((m) => m.id !== args.model_id);
        return {};
      }
      throw Error("unexpected IPC");
    },
  };
  return {
    state,
    api,
    rpc,
    save: async (s) => {
      saved = structuredClone(s);
    },
    snapshot: () => ({ maps, models, saved }),
  };
}
const catalogs = {
  codex: [{ key: "gpt-test", actual: "gpt-test", name: "Test" }],
};
test("idempotence and prune only owned models after successful empty catalog", async () => {
  const f = fixture();
  assert.equal(
    (await reconcile(f.state, catalogs, f.api, f.rpc, f.save)).length,
    2,
  );
  assert.equal(
    (await reconcile(f.state, catalogs, f.api, f.rpc, f.save)).length,
    0,
  );
  await f.api("/admin/model-mappings", "POST", {
    alias: "manual",
    provider: "codex",
    actual_model: "manual",
    display_name: "Manual",
  });
  await reconcile(f.state, { codex: [] }, f.api, f.rpc, f.save);
  assert.equal(f.state.entries.length, 0);
  assert.equal(f.snapshot().maps[0].alias, "manual");
});
test("failed or absent provider discovery preserves its owned records", async () => {
  const f = fixture();
  await reconcile(f.state, catalogs, f.api, f.rpc, f.save);
  await reconcile(f.state, { agy: [] }, f.api, f.rpc, f.save);
  assert.equal(f.state.entries.length, 1);
  assert.throws(() => staleEntries(f.state.entries, { codex: undefined }));
});
test("crash after proxy creation recovers journal without duplicate registration", async () => {
  const f = fixture();
  let crash = true;
  const interrupted = async (...args) => {
    const m = await f.api(...args);
    if (args[1] === "POST" && crash) {
      crash = false;
      throw Error("connection lost");
    }
    return m;
  };
  await assert.rejects(
    reconcile(f.state, catalogs, interrupted, f.rpc, f.save),
  );
  const resumed = f.snapshot().saved;
  await reconcile(resumed, catalogs, f.api, f.rpc, f.save);
  assert.equal(f.snapshot().maps.length, 1);
  assert.equal(f.snapshot().models.length, 1);
});
test("manual collisions and changed identities prevent mutations", async () => {
  const f = fixture();
  await f.api("/admin/model-mappings", "POST", {
    alias: "codex--gpt-test",
    provider: "codex",
    actual_model: "manual",
    display_name: "Manual",
  });
  await assert.rejects(
    reconcile(f.state, catalogs, f.api, f.rpc, f.save),
    /Unmanaged collision/,
  );
  assert.equal(f.state.entries.length, 0);
  assert.throws(
    () =>
      checkOwnership(
        {
          providerId: "p",
          entries: [{ provider: "codex", alias: "a", proxyId: "1" }],
        },
        [{ id: "1", alias: "other", provider: "codex" }],
        [],
      ),
    /identity changed/,
  );
});
test("partial deletion resumes with saved ownership", async () => {
  const f = fixture();
  await reconcile(f.state, catalogs, f.api, f.rpc, f.save);
  let fail = true;
  const interrupted = async (...args) => {
    if (args[1] === "DELETE" && fail) {
      fail = false;
      throw Error("offline");
    }
    return f.api(...args);
  };
  await assert.rejects(
    reconcile(f.state, { codex: [] }, interrupted, f.rpc, f.save),
  );
  assert.equal(f.state.entries.length, 1);
  await reconcile(f.state, { codex: [] }, f.api, f.rpc, f.save);
  assert.equal(f.state.entries.length, 0);
});
test("Agy distinguishes a complete empty catalog from blank output and errors", () => {
  assert.deepEqual(parseAgyModels("No models available"), []);
  assert.equal(parseAgyModels("model-1\tModel One")[0].actual, "model-1");
  assert.throws(() => parseAgyModels(""));
  assert.throws(() => parseAgyModels("model-1 Model", "Please sign in"));
  assert.throws(() =>
    validateCatalog("codex", [
      { key: "a", actual: "a", name: "A" },
      { key: "a", actual: "a", name: "A" },
    ]),
  );
});

test("partial failures report already completed changes", async () => {
  const f = fixture(),
    changes = [];
  const rpc = {
    call: async (type, args) => {
      if (type === "upsert_provider_model") throw Error("Copilot closed");
      return f.rpc.call(type, args);
    },
  };
  await assert.rejects(
    reconcile(f.state, catalogs, f.api, rpc, f.save, changes),
  );
  assert.deepEqual(changes, ["Added codex--gpt-test to proxy"]);
});

test("discovery distinguishes authentication skips from actual errors without exposing diagnostics", async () => {
  const { discover } = await import("./discovery.mjs");
  const results = [];
  const catalogs = await discover(results, async (provider) => {
    if (provider === "agy")
      throw Object.assign(Error("private account data"), {
        stderr: "Please sign in to view available models: secret",
      });
    if (provider === "grok") throw Error("private network diagnostic");
    return [];
  });
  assert.deepEqual(Object.keys(catalogs), ["codex", "claude"]);
  assert.equal(results[2].status, "failed");
  assert.equal(results[3].status, "skipped");
  assert.equal(results[3].reason, "not signed in");
  assert.doesNotMatch(JSON.stringify(results), /private|secret/);
});
