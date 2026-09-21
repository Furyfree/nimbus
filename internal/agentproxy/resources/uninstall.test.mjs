import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

test("native uninstall preserves user tools/config and refuses unsafe or failed removal", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "nimbus-uninstall-"));
  process.env.HOME = root;
  for (const key of ["XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"])
    process.env[key] = path.join(root, key);
  const bin = path.join(root, "bin");
  await fs.mkdir(bin);
  process.env.PATH = bin + ":" + process.env.PATH;
  const { uninstall } = await import("./uninstall.mjs");
  const { data, configDir, statePath } = await import("./storage.mjs");
  const { release } = await import("./setup.mjs");
  const unitDir = path.join(process.env.XDG_CONFIG_HOME, "systemd/user");
  const mock = `#!${process.execPath}
import fs from 'node:fs';
import path from 'node:path';
const args = process.argv.slice(2);
const unit = args.find(a => a.endsWith('.service'));
const file = unit && path.join(process.env.XDG_CONFIG_HOME, 'systemd/user', unit);
if (args.includes('show')) {
  const present = fs.existsSync(file);
  console.log('LoadState=' + (present ? 'loaded' : 'not-found'));
  console.log('ActiveState=' + (present && !fs.existsSync(file+'.stopped') ? 'active' : 'inactive'));
  console.log('FragmentPath=' + (present ? file : ''));
  console.log('DropInPaths=');
} else if (args.includes('disable') || args.includes('stop')) {
  if (process.env.FAIL_STOP) process.exit(1);
  if (file) fs.writeFileSync(file+'.stopped', '');
}
`;
  await fs.writeFile(path.join(bin, "systemctl"), mock, { mode: 0o700 });
  await fs.writeFile(path.join(bin, "herdr"), "keep Mise CLI");
  async function fixture() {
    await fs.mkdir(path.join(data, "releases", release), { recursive: true, mode: 0o700 });
    await fs.writeFile(path.join(data, "releases", release, "VERSION"), release);
    await fs.symlink(path.join(data, "releases", release), path.join(data, "current"));
    await fs.mkdir(unitDir, { recursive: true });
    for (const [name, entry] of [["agent-proxy", "index.js"], ["herdr", "herdr/server.js"]])
      await fs.writeFile(path.join(unitDir, name + ".service"), `ExecStart=/usr/bin/env node "${data}/current/packages/server/dist/${entry}"\n`);
    await fs.mkdir(configDir, { recursive: true });
    await fs.writeFile(path.join(configDir, "config.yaml"), "keep config");
    await fs.mkdir(path.dirname(statePath), { recursive: true, mode: 0o700 });
    await fs.writeFile(statePath, "{}", { mode: 0o600 });
  }
  const state = { providerId: "owned" };
  const report = () => ({ changes: [], notices: [] });
  const closed = async () => { throw Error("closed"); };
  try {
    await fixture();
    const unit = path.join(unitDir, "herdr.service");
    const original = await fs.readFile(unit);
    await fs.writeFile(unit, "ExecStart=/foreign/herdr\n");
    await assert.rejects(uninstall(state, report(), closed), /Unrecognized/);
    assert.equal(await fs.readFile(path.join(data, "current/VERSION"), "utf8"), release);
    await fs.writeFile(unit, original);
    process.env.FAIL_STOP = "1";
    await assert.rejects(uninstall(state, report(), closed));
    await fs.access(statePath);
    await fs.access(path.join(data, "current"));
    delete process.env.FAIL_STOP;
    const result = report();
    await uninstall(state, result, closed);
    assert.equal(result.status, "succeeded");
    assert.equal(result.notices.length, 1);
    await assert.rejects(fs.access(data));
    await assert.rejects(fs.access(statePath));
    assert.equal(await fs.readFile(path.join(configDir, "config.yaml"), "utf8"), "keep config");
    assert.equal(await fs.readFile(path.join(bin, "herdr"), "utf8"), "keep Mise CLI");
    await uninstall(null, report(), closed); // Retry after successful removal.
    await fixture();
    let providers = [{ id: "owned", kind: "openai", settings: { baseUrl: "http://127.0.0.1:8300/v1", wireApi: "completions" } }, { id: "foreign" }];
    await uninstall(state, report(), async () => ({
      close() {},
      async call(method, args) {
        if (method === "list_model_providers") return { providers };
        assert.equal(method, "delete_model_provider");
        assert.deepEqual(args, { provider_id: "owned" });
        providers = providers.filter(p => p.id !== args.provider_id);
      },
    }));
    assert.deepEqual(providers, [{ id: "foreign" }]);
  } finally { await fs.rm(root, { recursive: true, force: true }); }
});
