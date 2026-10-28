#!/usr/bin/env bash
# Instala o atualizador automático do FinPi no Raspberry Pi. Execute com: sudo bash install-updater.sh
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "Execute com sudo." >&2; exit 1; }

HERE="$(cd "$(dirname "$0")" && pwd)"

for cmd in curl sha256sum flock python3 systemctl; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "Dependência ausente: $cmd" >&2; exit 1; }
done

install -m 0755 -o root -g root "$HERE/finpi-update.sh" /usr/local/sbin/finpi-update
install -m 0644 -o root -g root "$HERE/finpi-update.service" /etc/systemd/system/finpi-update.service
install -m 0644 -o root -g root "$HERE/finpi-update.timer" /etc/systemd/system/finpi-update.timer

systemctl daemon-reload
systemctl enable --now finpi-update.timer

echo "Atualizador instalado. Próxima verificação:"
systemctl list-timers finpi-update.timer --no-pager
