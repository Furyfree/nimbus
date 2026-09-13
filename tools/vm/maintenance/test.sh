#!/bin/bash
set -euo pipefail
report() {
  local status=$?
  cat /work/plain.log /work/upgrade.log 2>/dev/null || :
  exit "$status"
}
trap report EXIT
[[ -f /.dockerenv && $(id -u) == 0 ]] || exit 99
mkdir -p /work/rpmbuild/{SPECS,SOURCES,RPMS,BUILD,BUILDROOT} /work/repo /work/gnupg
chmod 700 /work/gnupg
export GNUPGHOME=/work/gnupg
gpg --batch --passphrase '' --quick-generate-key 'Nimbus disposable test <test@example.invalid>' rsa2048 sign 1d
key=$(gpg --with-colons --list-keys | awk -F: '$1=="fpr" {print $10;exit}')
gpg --armor --export "$key" > /work/key.asc
rpmkeys --import /work/key.asc
for version in 0.4.6 0.4.7; do
  input=/input/nimbus-old
  [[ $version == 0.4.6 ]] || input=/input/nimbus-new
  cp "$input" /work/rpmbuild/SOURCES/nimbus
  cat > /work/rpmbuild/SPECS/nimbus.spec <<SPEC
Name: nimbus
Version: $version
Release: 1.fc44
Summary: Disposable maintenance test engine
License: MIT
Source0: nimbus
%description
Disposable test only.
%install
mkdir -p %{buildroot}/usr/bin
install -m 755 %{SOURCE0} %{buildroot}/usr/bin/nimbus
%files
/usr/bin/nimbus
SPEC
  rpmbuild --define '_topdir /work/rpmbuild' --define '__brp_strip /bin/true' --define '__brp_strip_comment_note /bin/true' -bb /work/rpmbuild/SPECS/nimbus.spec
  rpmsign --define '_openpgp_sign gpg' --define "_openpgp_sign_id $key" --addsign /work/rpmbuild/RPMS/x86_64/nimbus-$version-1.fc44.x86_64.rpm
 done
rpm -i /work/rpmbuild/RPMS/x86_64/nimbus-0.4.6-1.fc44.x86_64.rpm
# A private signed test source replaces repositories only inside this container.
rm /etc/yum.repos.d/*.repo
cat > /etc/yum.repos.d/nimbus-engine.repo <<REPO
[nimbus-engine]
name=Disposable Nimbus test
baseurl=file:///work/repo
enabled=1
gpgcheck=1
gpgkey=file:///work/key.asc
skip_if_unavailable=0
metadata_expire=7d
REPO
cp /work/rpmbuild/RPMS/x86_64/nimbus-0.4.6-1.fc44.x86_64.rpm /work/repo/
createrepo_c /work/repo
dnf5 makecache
# Publish 0.4.7 after root's still-valid seven-day cache was created.
cp /work/rpmbuild/RPMS/x86_64/nimbus-0.4.7-1.fc44.x86_64.rpm /work/repo/
createrepo_c --update /work/repo
[[ -z $(dnf5 -q --cacheonly repoquery --upgrades --qf "%{full_nevra}" nimbus) ]]
useradd -m tester
mkdir -m 700 /home/tester/runtime
printf 'tester ALL=(ALL) NOPASSWD: ALL\n' > /etc/sudoers.d/tester
chmod 440 /etc/sudoers.d/tester
mkdir /home/tester/checkout
printf 'future_unknown_field=true\n' > /home/tester/checkout/nimbus.toml
chown -R tester:tester /home/tester
set +e
runuser -u tester -- env XDG_RUNTIME_DIR=/home/tester/runtime /usr/bin/nimbus sync --yes --checkout /home/tester/checkout --machine vm > /work/plain.log 2>&1
plain=$?
set -e
[[ $plain == 1 ]]
grep -q 'Nimbus update available' /work/plain.log
if grep -q 'definition error' /work/plain.log; then exit 1; fi
[[ $(rpm -q --qf '%{VERSION}' nimbus) == 0.4.6 ]]
set +e
runuser -u tester -- env XDG_RUNTIME_DIR=/home/tester/runtime /usr/bin/nimbus sync --upgrade --yes --prune --checkout /home/tester/checkout --machine vm > /work/upgrade.log 2>&1
upgrade=$?
set -e
[[ $upgrade == 1 ]]
[[ $(rpm -q --qf '%{VERSION}' nimbus) == 0.4.7 ]]
grep -q 'restarting before configuration sync' /work/upgrade.log
grep -q 'definition error' /work/upgrade.log
# Engine update must precede new-definition validation, and its failure must
# preserve the completed RPM update in the report.
grep -q 'updated.*Nimbus: nimbus-0:0.4.7' /work/upgrade.log
printf 'PASS: signed RPM upgrade, stale root cache refreshed, restart and selection preservation, later failure reporting\n'
