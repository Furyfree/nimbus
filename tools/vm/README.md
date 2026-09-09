# Testing an unpublished candidate

Use a local candidate for Phase 6 development. It tests uncommitted code and
definitions without a release, a Git push, or changes to stable COPR. A separate
beta COPR project can later test RPM installation and upgrade behavior with
published source; it is unnecessary for the first engine/resource drills.

Run the complete local gate first. Then, when VM staging is authorized:

~~~sh
just vm-stage
~~~

The default destination is `pby@127.0.0.1`, port `2222`; both are recipe
arguments. The developer machine needs Git, Go, Python 3, and SSH. The VM needs
Python 3 and an authenticated SSH connection. Staging does not install these.

The tool builds a static Linux x86_64 binary from a temporary source snapshot.
It includes modified tracked files and non-ignored new files under the source
and definition directories. Untracked root files and ignored state are excluded.
Review additions before staging. Git HEAD and the original tracked index are
preserved, so modified definitions remain visibly dirty. A fresh shallow Git
database replaces local Git configuration, hooks, and reflogs. No commit is
created, and the developer checkout is unchanged.

Each run creates a fresh private `~/.nimbus-candidate-*` directory on the VM.
The binary checksum is verified after transfer. Staging never executes the
candidate, updates the selector, overwrites `/usr/bin/nimbus`, adds a PATH
shadow, or replaces `~/.local/share/nimbus`. Failed extraction removes only its
own newly created directory. Earlier candidates remain available for comparison.

To build without contacting the VM, use a destination that does not exist:

~~~sh
python3 -I -B tools/vm/stage.py --output /tmp/nimbus-phase6-candidate
~~~

## VM drill

Use a disposable Fedora VM snapshot. Staging prints the exact candidate path;
set it below, then run the read-only checks with an explicit machine:

~~~sh
candidate="$HOME/.nimbus-candidate-REPLACE_WITH_PRINTED_SUFFIX"
"$candidate/nimbus" version
"$candidate/nimbus" validate --checkout "$candidate/checkout"
"$candidate/nimbus" doctor --checkout "$candidate/checkout" --machine vm
"$candidate/nimbus" sync --plan --checkout "$candidate/checkout" --machine vm
~~~

Review the complete plan before applying it in the disposable VM:

~~~sh
"$candidate/nimbus" sync --checkout "$candidate/checkout" --machine vm
~~~

Explicit checkout/machine flags avoid changing the installed selector. Sync
still modifies the VM's real packages, services, files, and Nimbus receipts;
candidate staging is isolation of delivery, not isolation of system effects.
Recover the VM snapshot between destructive or failed ownership drills. Do not
run an older stable engine against candidate-written state unless compatibility
has been established.
The first successful state write upgrades the schema marker to 2, which
0.1.1 refuses. Failures before that write still require snapshot restoration;
never reset the marker manually to make an older engine accept the state.

Verify reboot to the greeter, normal login/logout, portal file selection and
screen sharing, and recovery with missing or broken user configuration. Repeat
sync must show no unexpected changes. Exercise failed activation and safe
removal while preserving console access. Record the candidate checksum,
definition digest, native service state, logs, and observed results.

For the first-install flow, use the candidate binary's
`init --checkout "$candidate/checkout" --machine vm` in a clean VM. Init shows
and applies its plan without confirmation. It writes a selector and refuses
an existing selector with a different checkout or origin without changing it.
Testing the public shell/RPM bootstrap is
a separate packaging drill; a direct binary candidate does not prove COPR
publication or signed RPM installation.
