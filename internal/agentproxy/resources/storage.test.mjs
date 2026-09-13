import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { atomic, privateRead, validateState } from "./storage.mjs";
test("private storage refuses symlinks, shared files, and foreign-machine state", async () => {
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), "nimbus-proxy-test-"));
  try {
    const file = path.join(temp, "state");
    await atomic(file, { version: 1 });
    assert.equal(JSON.parse(await privateRead(file)).version, 1);
    await fs.chmod(file, 0o644);
    await assert.rejects(atomic(file, {}));
    await fs.chmod(file, 0o600);
    await fs.symlink(file, path.join(temp, "link"));
    await assert.rejects(privateRead(path.join(temp, "link")));
    assert.throws(() =>
      validateState(
        {
          version: 1,
          machine: "other",
          providerId: "a",
          configured: true,
          entries: [],
        },
        "test",
      ),
    );
  } finally {
    await fs.rm(temp, { recursive: true, force: true });
  }
});
