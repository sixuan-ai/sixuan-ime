#!/usr/bin/env bash
set -euo pipefail
umask 022
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
version=1.2.5
arch=$(dpkg --print-architecture)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
stage="$work/package"
mkdir -p "$root/dist" "$stage/DEBIAN" "$stage/usr/bin" \
  "$stage/usr/lib/systemd/user" "$stage/usr/share/ibus/component" \
  "$stage/usr/share/doc/sixuan-ime" "$stage/usr/share/gnome-shell/extensions"
go -C "$root" build -buildvcs=false -trimpath -o "$stage/usr/bin/sixuan-ime" .
cat > "$stage/usr/lib/systemd/user/sixuan-ime.service" <<'UNIT'
[Unit]
Description=Sixuan IME
PartOf=graphical-session.target
After=graphical-session.target
[Service]
Type=simple
ExecStart=/usr/bin/sixuan-ime daemon
Restart=on-failure
RestartSec=2
[Install]
WantedBy=graphical-session.target
UNIT
cat > "$stage/usr/share/ibus/component/sixuan-ime.xml" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<component>
  <name>org.freedesktop.IBus.SixuanIME</name>
  <description>思玄输入法</description>
  <exec>/usr/bin/systemctl --user start sixuan-ime.service</exec>
  <version>1.2.5</version>
  <license>PolyForm-Noncommercial-1.0.0</license>
  <author>Shenzhen Sixuan Technology Co., Ltd.</author>
  <engines>
    <engine><name>sixuan-pinyin</name><longname>思玄拼音</longname><description>Rime 本地拼音</description><language>zh</language><license>PolyForm-Noncommercial-1.0.0</license><author>Sixuan</author><icon>input-keyboard</icon><layout>default</layout><setup>/usr/bin/sixuan-ime settings</setup></engine>
    <engine><name>sixuan-voice</name><longname>思玄语音</longname><description>语音输入</description><language>zh</language><license>PolyForm-Noncommercial-1.0.0</license><author>Sixuan</author><icon>audio-input-microphone</icon><layout>default</layout><setup>/usr/bin/sixuan-ime settings</setup></engine>
  </engines>
</component>
XML
cp -r "$root/desktop/gnome-shell/sixuan-candidates@sixuan.dev" "$stage/usr/share/gnome-shell/extensions/"
for name in LICENSE.md NOTICE THIRD_PARTY_NOTICES.md README.md; do
  cp "$root/$name" "$stage/usr/share/doc/sixuan-ime/"
done
cat > "$stage/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if [ "$1" = configure ] && command -v ibus >/dev/null 2>&1; then
    ibus write-cache --system >/dev/null 2>&1 || true
fi
exit 0
POSTINST
chmod 0755 "$stage/DEBIAN/postinst"
mkdir "$work/debian"
cat > "$work/debian/control" <<CONTROL
Source: sixuan-ime
Section: utils
Priority: optional
Maintainer: Sixuan <support@sixuanai.com>

Package: sixuan-ime
Architecture: $arch
Description: Sixuan IME
CONTROL
shlibs=$(cd "$work" && dpkg-shlibdeps -O -e"$stage/usr/bin/sixuan-ime")
cat > "$stage/DEBIAN/control" <<CONTROL
Package: sixuan-ime
Version: $version
Section: utils
Priority: optional
Architecture: $arch
Maintainer: Sixuan <support@sixuanai.com>
Homepage: https://github.com/sixuan-ai/sixuan-ime
Depends: ${shlibs#shlibs:Depends=}, ibus, rime-data-luna-pinyin
Recommends: ffmpeg
Installed-Size: $(du -sk "$stage/usr" | cut -f1)
Description: Sixuan pinyin and voice input for Ubuntu GNOME
 Local pinyin input and voice input using a user-configured service.
CONTROL
file="sixuan-ime_${version}_${arch}.deb"
dpkg-deb --build --root-owner-group "$stage" "$root/dist/$file"
(cd "$root/dist" && sha256sum "$file" > SHA256SUMS)
echo "Built dist/$file"
