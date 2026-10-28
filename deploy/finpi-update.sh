#!/usr/bin/env bash
# finpi-update: busca a última release do FinPi no GitHub e atualiza o serviço local.
#
# Fluxo: consulta a release -> baixa binário + SHA256SUMS -> confere checksum e formato ->
# faz backup do banco -> troca o binário e reinicia -> confere se o app responde.
# Se não responder, volta o binário e o banco anteriores.
#
# Pensado para rodar como root via systemd timer (finpi-update.timer).
set -euo pipefail

REPO="${FINPI_REPO:-Deividi-Lucas/FinPi}"
API_URL="${FINPI_API_URL:-https://api.github.com}"
DOWNLOAD_URL="${FINPI_DOWNLOAD_URL:-https://github.com}"
DIR="${FINPI_DIR:-/opt/finpi}"
SERVICE="${FINPI_SERVICE:-finpi}"
HEALTH_URL="${FINPI_HEALTH_URL:-http://127.0.0.1:8080/}"
HEALTH_TIMEOUT="${FINPI_HEALTH_TIMEOUT:-30}"
KEEP_BACKUPS="${FINPI_KEEP_BACKUPS:-14}"
SYSTEMCTL="${FINPI_SYSTEMCTL:-systemctl}"

ASSET="finpi-linux-arm64"
BIN="$DIR/finpi"
DB="$DIR/data/finpi.db"
BACKUP_DIR="$DIR/backups"
VERSION_FILE="$DIR/VERSION"
FAILED_FILE="$DIR/FAILED_VERSION"

log() { printf '[finpi-update] %s\n' "$*"; }
die() { log "ERRO: $*"; exit 1; }

for cmd in curl sha256sum flock python3; do
  command -v "$cmd" >/dev/null 2>&1 || die "dependência ausente: $cmd"
done

mkdir -p "$DIR"
exec 9>"$DIR/.update.lock"
flock -n 9 || { log "outra atualização está em andamento"; exit 0; }

OWNER="$(stat -c '%u:%g' "$DIR")"

# --- 1. Qual é a última release? ---------------------------------------------
response="$(curl -fsS --max-time 30 -H 'Accept: application/vnd.github+json' \
  "$API_URL/repos/$REPO/releases/latest")" || die "não foi possível consultar a última release"
TAG="$(printf '%s' "$response" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
[[ "$TAG" =~ ^[A-Za-z0-9._-]+$ ]] || die "tag de release inválida: '$TAG'"

current="$(cat "$VERSION_FILE" 2>/dev/null || echo none)"
if [ "$TAG" = "$current" ]; then
  log "já está na versão $TAG"
  exit 0
fi
if [ "$TAG" = "$(cat "$FAILED_FILE" 2>/dev/null || true)" ]; then
  log "versão $TAG já falhou antes; aguardando uma release mais nova"
  exit 0
fi
log "nova versão disponível: $current -> $TAG"

# --- 2. Baixa e confere -------------------------------------------------------
TMP="$(mktemp -d "$DIR/.update.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

base="$DOWNLOAD_URL/$REPO/releases/download/$TAG"
curl -fsSL --max-time 300 -o "$TMP/$ASSET" "$base/$ASSET" || die "falha ao baixar $ASSET"
curl -fsSL --max-time 30 -o "$TMP/SHA256SUMS" "$base/SHA256SUMS" || die "falha ao baixar SHA256SUMS"

(cd "$TMP" && grep " $ASSET\$" SHA256SUMS | sha256sum -c --status -) \
  || die "checksum do binário não confere"

# ELF (7f 45 4c 46) com e_machine = 0xB7 (aarch64), nos bytes 18-19 do cabeçalho.
header="$(head -c 20 "$TMP/$ASSET" | od -An -tx1 | tr -d ' \n')"
[[ "$header" == 7f454c46* && "${header:36:4}" == "b700" ]] \
  || die "o arquivo baixado não é um executável ELF aarch64"

# --- 3. Backup do banco -------------------------------------------------------
LAST_BACKUP=""
if [ -f "$DB" ]; then
  mkdir -p "$BACKUP_DIR"
  LAST_BACKUP="$BACKUP_DIR/finpi-$(date +%Y%m%d-%H%M%S).db"
  python3 - "$DB" "$LAST_BACKUP" <<'PY' || die "falha ao fazer backup do banco; atualização cancelada"
import sqlite3, sys
src = sqlite3.connect("file:%s?mode=ro" % sys.argv[1], uri=True)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst)
ok = dst.execute("pragma integrity_check").fetchone()[0]
dst.close()
src.close()
sys.exit(0 if ok == "ok" else 1)
PY
  chown "$OWNER" "$LAST_BACKUP"
  log "backup do banco: $LAST_BACKUP"
  # Mantém só os KEEP_BACKUPS mais recentes.
  ls -1t "$BACKUP_DIR"/finpi-*.db 2>/dev/null | tail -n +"$((KEEP_BACKUPS + 1))" | xargs -r rm -f --
fi

# --- 4. Troca o binário e reinicia -------------------------------------------
wait_healthy() {
  local i
  for ((i = 0; i < HEALTH_TIMEOUT; i++)); do
    if "$SYSTEMCTL" is-active --quiet "$SERVICE" \
      && curl -fsS -o /dev/null --max-time 3 "$HEALTH_URL"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

[ -f "$BIN" ] && cp -p "$BIN" "$BIN.prev"
install -m 0755 "$TMP/$ASSET" "$BIN.new"
chown "$OWNER" "$BIN.new"
mv -f "$BIN.new" "$BIN"

log "reiniciando $SERVICE"
"$SYSTEMCTL" restart "$SERVICE" || true

if wait_healthy; then
  printf '%s\n' "$TAG" > "$VERSION_FILE"
  rm -f "$FAILED_FILE"
  log "atualizado para $TAG com sucesso"
  exit 0
fi

# --- 5. Rollback --------------------------------------------------------------
log "ERRO: o app não respondeu após a atualização; revertendo"
"$SYSTEMCTL" stop "$SERVICE" || true
[ -f "$BIN.prev" ] && mv -f "$BIN.prev" "$BIN"
if [ -n "$LAST_BACKUP" ] && [ -f "$LAST_BACKUP" ]; then
  # A nova versão pode ter migrado o banco; o binário antigo precisa do banco antigo.
  rm -f "$DB" "$DB-wal" "$DB-shm"
  cp -p "$LAST_BACKUP" "$DB"
  chown "$OWNER" "$DB"
fi
"$SYSTEMCTL" start "$SERVICE" || true
printf '%s\n' "$TAG" > "$FAILED_FILE"

if wait_healthy; then
  log "versão anterior restaurada ($current); $TAG marcada como falha"
else
  log "CRÍTICO: o app não responde nem após o rollback; verifique 'journalctl -u $SERVICE'"
fi
exit 1
