# Bundled data notices

The vendored `github.com/rivo/uniseg` v0.4.7 tables include Unicode 15.0.0
character data under Unicode-DFS-2016, separately from that module's MIT
license. `Unicode-DFS-2016.txt` preserves the Unicode notice distributed with
[ICU 72.1](https://github.com/unicode-org/icu/blob/release-72-1/icu4c/LICENSE),
which uses Unicode 15.0.0.

Source releases retain vendored dependency notices. The RPM also installs
this supplemental notice alongside those notices and the Go compiler license.
Nimbus's own code remains covered by the root MIT license.

The DTU helper `internal/postinstall/dtu/network.py` adapts the NetworkManager
path from [GEANT CAT's DTU Linux installer][dtu-cat]. That helper retains its
upstream notices and is covered by [GEANT-CAT.txt](GEANT-CAT.txt), the GEANT
Standard Open Source Software Outward Licence. Other Nimbus code retains its
MIT licence. Include this notice and account for the helper's licence when
packaging the new DTU candidate.

Reviewed 2026-09-16: provider 533, profile 863, DTU Non Windows rev2022-10-04.
Downloaded installer SHA-256:
`fa743351af644434695dcdf4418bf8104050fb1874de47947141e8164f4d3b79`.
Nimbus embeds the extracted public CA bundle and adapted native settings;
it does not execute downloaded code. Modifications replace CAT's same-SSID
profile deletion with explicit approval and fresh observations, add private
credential
input and offline inspection, and separate preparation from activation.

[dtu-cat]: https://cat.eduroam.org/user/API.php?action=downloadInstaller&device=linux&profile=863&lang=en
