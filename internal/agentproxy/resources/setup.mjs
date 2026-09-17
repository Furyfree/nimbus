import { createRequire } from "node:module";
import { validateConfig, localAPI } from "./proxy.mjs";
import { Failure } from "./storage.mjs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { data, configDir, ancestors } from "./storage.mjs";
export const release = "1.3.0-nimbus.2-source";
const commit = "66cc75af597f07563384be9abc6790b3ee69d32f";
const checksum =
  "b2524276cde75e081af5c983fc620eb17b6f167812042d09502a5c39417ed14d";
const resources = path.dirname(fileURLToPath(import.meta.url));
export async function command(name, args, cwd) {
  process.stderr.write(`-> ${name} ${args.join(" ")}\n`);
  // Reuse the selected Node runtime inside npm and upstream scripts.
  const env = {
    ...process.env,
    PATH: path.dirname(process.execPath) + ":" + process.env.PATH,
    CI: "1",
  };
  await new Promise((resolve, reject) => {
    const child = spawn(name, args, { cwd, env, stdio: ["ignore", 2, 2] });
    const timer = setTimeout(() => child.kill("SIGTERM"), 15 * 60 * 1000);
    child.on("error", () => {
      clearTimeout(timer);
      reject(Error(`Cannot start ${name}`));
    });
    child.on("exit", (code) => {
      clearTimeout(timer);
      code === 0
        ? resolve()
        : reject(Error(`${name} failed; inspect its output above`));
    });
  });
}
export async function installedRelease() {
  try {
    return (
      await fs.readFile(path.join(data, "current/VERSION"), "utf8")
    ).trim();
  } catch (e) {
    if (e.code === "ENOENT") return "";
    throw e;
  }
}
export async function install() {
  if (Number(process.versions.node.split(".")[0]) < 24)
    throw new Failure("Node 24 or newer is required; apply the Mise selection");
  for (const dir of [data, configDir]) await ancestors(path.join(dir, "check"));
  const current = await installedRelease();
  for (const unit of ["agent-proxy.service", "herdr.service"]) {
    const file = path.join(path.dirname(configDir), "systemd/user", unit);
    try {
      const st = await fs.lstat(file);
      const text = await fs.readFile(file, "utf8");
      if (
        !current ||
        st.isSymbolicLink() ||
        !st.isFile() ||
        st.uid !== process.getuid() ||
        !text.includes(path.join(data, "current/packages/server/dist/"))
      )
        throw new Failure(
          "Unrecognized existing user service; refusing replacement",
        );
    } catch (e) {
      if (e.code !== "ENOENT") throw e;
    }
  }

  if (current === release) return false;
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), "nimbus-proxy-build-"));
  try {
    process.stderr.write(
      `Downloading verified agent-proxy source ${commit}...\n`,
    );
    const response = await fetch(
      `https://codeload.github.com/ChrisTitusTech/agent-proxy/tar.gz/${commit}`,
      { signal: AbortSignal.timeout(60000) },
    );
    if (!response.ok)
      throw new Failure(`Source download failed: HTTP ${response.status}`);
    const archive = Buffer.from(await response.arrayBuffer());
    if (createHash("sha256").update(archive).digest("hex") !== checksum)
      throw new Failure("Source checksum mismatch; installation refused");
    await fs.writeFile(path.join(temp, "source.tar.gz"), archive, {
      mode: 0o600,
    });
    const source = path.join(temp, "source");
    await fs.mkdir(source);
    await command(
      "tar",
      [
        "--extract",
        "--gzip",
        "--file",
        path.join(temp, "source.tar.gz"),
        "--strip-components=1",
        "--directory",
        source,
      ],
      temp,
    );
    await command(
      "git",
      ["apply", "--check", path.join(resources, "upstream.patch")],
      source,
    );
    await command(
      "git",
      ["apply", path.join(resources, "upstream.patch")],
      source,
    );
    await command("npm", ["ci"], source);
    await command("npm", ["run", "build"], source);
    await command(
      "bash",
      [
        "scripts/build-release.sh",
        "--skip-build",
        "--output",
        path.join(temp, "release"),
      ],
      source,
    );
    const archives = (await fs.readdir(path.join(temp, "release"))).filter(
      (x) => x.endsWith(".tar.gz"),
    );
    if (archives.length !== 1)
      throw new Failure("Expected one native release archive");
    const require = createRequire(path.join(source, "package.json"));
    validateConfig(
      require("yaml").parse(
        await fs.readFile(path.join(configDir, "config.yaml"), "utf8"),
      ),
    );
    if (current) {
      const { api } = await localAPI();
      const active = await api("/admin/active-requests");
      if (active.count !== 0)
        throw new Failure(
          "Proxy became busy while building; retry after requests finish",
        );
    }
    await command(
      "bash",
      [
        "scripts/install.sh",
        current ? "upgrade" : "install",
        "--archive",
        path.join(temp, "release", archives[0]),
      ],
      source,
    );
    if ((await installedRelease()) !== release)
      throw new Failure("Native release activation was not verified");
    return true;
  } finally {
    await fs.rm(temp, { recursive: true, force: true });
  }
}
