# Manual releases

Stable releases and COPR builds are separate authorized operations. A tag
does not publish a stable release. Pushing to `develop` does publish rolling
source assets through the develop workflow. The local worktree is not release input;
only the selected committed tag is exported.

## Release steps

1. Merge the reviewed work, then use a clean, current `main` checkout. Choose
   a new stable tag in the form `vMAJOR.MINOR.PATCH`.
2. Run `just tag TAG` with that version. This creates an annotated tag and
   pushes it. It refuses a dirty checkout, a main commit different from the
   remote, or a tag already on the remote.
3. Run `just release TAG`. This dispatches **Prepare release** on GitHub
   Actions. It checks the selected commit, prepares vendored source and creates
   a draft. It does not publish it.
4. Inspect the draft's source, dependency notices and release notes. The assets
   are `nimbus-VERSION-vendor.tar.gz`, `RELEASE-SOURCE.txt` and `SHA256SUMS`.
   Use the attached vendored archive for COPR, not GitHub's automatic archive.
5. Publish the reviewed release. Update the Nimbus package version in the COPR
   repository and use that repository's current build workflow. Packaging and
   publication instructions belong there; a GitHub release alone is not an
   available signed RPM.
6. Verify the published RPM and bootstrap compatibility, then run the intended
   installation/update trial. Record the version, build and results in the
   release notes or linked issue; keep only unfinished checks in [TASKS.md](../../docs/TASKS.md).

The workflow reads the Go version from `go.mod`, runs `just check`, exports
committed source and tests the vendored build without downloads. Only the draft
job gets permission to create the release after validation. It checks that the
remote tag still identifies the tested commit.

## Failure and retry

A failed tag push leaves the local annotated tag. Retry can reuse it only if
it still identifies the same main commit. Never move a published tag to fix a
release; use a new version.

A failed source build creates no release. A failed upload can leave an
incomplete draft: inspect it before removing or retrying it. Existing releases
and assets are not overwritten. Successful draft creation still requires
publication and a separate COPR build.

## Local source preparation

For an existing tag and a new output directory outside the checkout:

~~~sh
bash tools/release/source.sh TAG /absolute/new-output-directory
~~~

The develop channel's rolling source comes from the current `origin/develop`
commit. Its version is `<develop-version>~dev.<UTC commit time>`; the
`Prepare develop source` workflow runs this on every push to `develop`:

~~~sh
bash tools/release/source.sh --channel develop /absolute/new-output-directory
~~~

This downloads modules and compiles in temporary storage. It does not publish,
install, or modify the working tree. The GitHub workflow also runs the complete
local gate on the selected commit.

`develop-version` names the next stable release. Stable tags must number at
or above it so a develop installation can return to stable by normal upgrade.
After a stable release, advance this base on `develop`.
