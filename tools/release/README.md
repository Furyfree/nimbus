# Manual Nimbus releases

`Prepare release` runs only when started manually from GitHub Actions on `main`.
Pushing commits or tags does not release Nimbus. Merge this workflow and the
reviewed engine changes before using it.

## First release

1. Switch to the reviewed main branch, update it, and create/push the tag:

   ~~~sh
   git switch main
   git pull --ff-only origin main
   just tag v0.1.0
   ~~~

   `just tag` requires a clean main checkout matching the remote main commit.
   It creates an annotated tag and pushes only that tag. Existing remote tags
   are refused. If a push fails, the local tag remains; retrying can reuse it
   only when it is annotated and still points at the same main commit.

2. Start release preparation from the Nimbus checkout with an authenticated
   GitHub CLI:

   ~~~sh
   just release v0.1.0
   ~~~

   This dispatches `Prepare release` on remote `main` with the existing tag.
   It does not create or push a tag, include local edits, or publish a release.
   Alternatively, in **Furyfree/nimbus > Actions > Prepare release > Run
   workflow**, select branch `main` and enter `v0.1.0` in `tag`.

3. Wait for the workflow. It checks that the tag is a stable `vMAJOR.MINOR.PATCH`
   version on main, runs `just check` on that commit, exports only committed
   files, vendors the Go modules, and builds/tests with downloads disabled.
   Changed module declarations, failed tests, or invalid definitions stop it.
4. Open **Releases** and inspect the resulting draft. Its assets are:

   - `nimbus-0.1.0-vendor.tar.gz`: source and dependency notices, rooted at
     `nimbus-0.1.0/`, ready for the COPR spec.
   - `RELEASE-SOURCE.txt`: the tag, exact commit, Go version, and module list.
   - `SHA256SUMS`: checksums of both files above.

5. Review the source, dependency licenses, and release notes, then click
   **Publish release**. The draft is not anonymously downloadable by COPR.
   GitHub's automatically generated source archives do not contain the
   generated vendor directory; use our attached archive.
6. In **Furyfree/copr > Actions > COPR > Run workflow**, select `main` and
   operation `build`. Its spec must have the same version as the release.
   The workflow prepares a source RPM; Fedora COPR compiles and tests it,
   signs the RPM, and publishes it in `furyfree/nimbus`.

The COPR workflow's `project` operation can run before the Nimbus release.
For the first installation, also verify that project's real public key and
ship its fingerprint in Nimbus. The signed RPM and reviewed bootstrap must be
available before the restored VM runs `~/start-phase5`.

## Failure and retry

The source job has read-only repository permissions. Only a separate job
receives permission to create the draft, after the checks pass. It checks
that the remote tag still points at the tested commit and never overwrites
an existing release or its assets. It does not create or move tags.

A failed source build creates no release. A failed upload can leave an
incomplete draft: inspect it, remove that draft manually if appropriate, and
rerun the workflow for the same unchanged tag. Keep published tags and assets
unchanged; subsequent fixes get a new version and a matching COPR spec update.
A successful run leaves a draft awaiting your publication, not a running COPR
build. Even publication leaves the COPR dispatch manual.

For local preparation, from a clone containing the tag and `origin/main`, run:

~~~sh
bash tools/release/source.sh v0.1.0 /absolute/new-output-directory
~~~

This runs Go module downloads and compilation in temporary storage. The
output directory must not already exist and must be outside the checkout.
The script does not publish, install, or change the working tree. The
workflow additionally runs the complete local gate on the selected commit.
