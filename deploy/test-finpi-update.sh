#!/usr/bin/env bash
# Testa deploy/finpi-update.sh contra um "GitHub" falso (http.server) e um systemctl falso.
# Requer: bash, curl, python3, flock, sha256sum (Linux).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
UPDATER="$HERE/finpi-update.sh"
WORK="$(mktemp -d)"
PORT=$((20000 + RANDOM % 20000))
REPO="test/finpi"
SITE="$WORK/site"
DIR="$WORK/opt"
BIN_DIR="$WORK/bin"
LOG="$WORK/systemctl.log"
SERVER_PID=""

cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

mkdir -p "$SITE" "$DIR/data" "$BIN_DIR"
: > "$LOG"
touch "$SITE/ok"

# systemctl falso: registra as chamadas e permite simular o efeito de restart/start.
cat > "$BIN_DIR/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$FAKE_SYSTEMCTL_LOG"
case "$1" in
  restart) [ -z "${FAKE_RESTART_HOOK:-}" ] || "$FAKE_RESTART_HOOK" ;;
  start)   [ -z "${FAKE_START_HOOK:-}" ] || "$FAKE_START_HOOK" ;;
esac
exit 0
EOF
# Simula uma versão quebrada: o app para de responder e o banco é alterado (migração).
cat > "$BIN_DIR/break.sh" <<EOF
#!/usr/bin/env bash
rm -f "$SITE/ok"
python3 -c "import sqlite3; c=sqlite3.connect('$DIR/data/finpi.db'); c.execute('delete from t'); c.commit()"
EOF
cat > "$BIN_DIR/heal.sh" <<EOF
#!/usr/bin/env bash
touch "$SITE/ok"
EOF
chmod +x "$BIN_DIR"/*

python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$SITE" >/dev/null 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
  curl -fsS -o /dev/null "http://127.0.0.1:$PORT/ok" 2>/dev/null && break
  sleep 0.1
done

# publish TAG CONTEUDO [bad_sum]: cria a release falsa (API + arquivos de download).
publish() {
  local tag=$1 content=$2 dl="$SITE/$REPO/releases/download/$1"
  mkdir -p "$SITE/repos/$REPO/releases" "$dl"
  printf '{\n  "tag_name": "%s",\n  "name": "%s"\n}\n' "$tag" "$tag" > "$SITE/repos/$REPO/releases/latest"
  { printf '\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\xb7\x00'; printf '%s' "$content"; } > "$dl/finpi-linux-arm64"
  (cd "$dl" && sha256sum finpi-linux-arm64 > SHA256SUMS)
  if [ "${3:-}" = bad_sum ]; then
    echo "0000000000000000000000000000000000000000000000000000000000000000  finpi-linux-arm64" > "$dl/SHA256SUMS"
  fi
}

run_updater() {
  FINPI_REPO="$REPO" FINPI_API_URL="http://127.0.0.1:$PORT" FINPI_DOWNLOAD_URL="http://127.0.0.1:$PORT" \
  FINPI_DIR="$DIR" FINPI_SYSTEMCTL="$BIN_DIR/systemctl" FINPI_HEALTH_URL="http://127.0.0.1:$PORT/ok" \
  FINPI_HEALTH_TIMEOUT=3 FINPI_KEEP_BACKUPS=2 FAKE_SYSTEMCTL_LOG="$LOG" \
  "$UPDATER"
}

FAILS=0
check() { # descrição, comando...
  local desc=$1; shift
  if "$@"; then echo "  ok   - $desc"; else echo "  FAIL - $desc"; FAILS=$((FAILS + 1)); fi
}
payload() { tail -c +21 "$DIR/finpi"; }
count() { grep -c "$1" "$LOG" || true; }
db_rows() { python3 -c "import sqlite3; print(sqlite3.connect('$DIR/data/finpi.db').execute('select count(*) from t').fetchone()[0])"; }

# Estado inicial: versão build-1 instalada, banco com 3 linhas.
{ printf '\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\xb7\x00'; printf 'OLD'; } > "$DIR/finpi"
echo build-1 > "$DIR/VERSION"
python3 -c "import sqlite3; c=sqlite3.connect('$DIR/data/finpi.db'); c.execute('create table t(x)'); c.executemany('insert into t values (?)', [(1,),(2,),(3,)]); c.commit()"

echo "1) já está na última versão"
publish build-1 OLD
run_updater >/dev/null
check "não reinicia o serviço" test "$(count restart)" -eq 0
check "binário intocado" test "$(payload)" = OLD

echo "2) nova versão saudável"
publish build-2 NEW2
run_updater >/dev/null
check "binário trocado" test "$(payload)" = NEW2
check "VERSION atualizado" test "$(cat "$DIR/VERSION")" = build-2
check "reiniciou o serviço" test "$(count restart)" -eq 1
check "backup do banco criado" test "$(ls "$DIR/backups" | wc -l)" -eq 1

echo "3) checksum inválido"
publish build-3 NEW3 bad_sum
if run_updater >/dev/null; then rc=0; else rc=$?; fi
check "falha (exit != 0)" test "$rc" -ne 0
check "binário intocado" test "$(payload)" = NEW2
check "não reiniciou" test "$(count restart)" -eq 1

echo "4) versão quebrada: rollback do binário e do banco"
publish build-4 NEW4
sleep 1.1 # garante nome de backup distinto
if FAKE_RESTART_HOOK="$BIN_DIR/break.sh" FAKE_START_HOOK="$BIN_DIR/heal.sh" run_updater >/dev/null 2>&1; then rc=0; else rc=$?; fi
check "falha (exit != 0)" test "$rc" -ne 0
check "binário anterior restaurado" test "$(payload)" = NEW2
check "banco restaurado (3 linhas)" test "$(db_rows)" -eq 3
check "VERSION continua build-2" test "$(cat "$DIR/VERSION")" = build-2
check "build-4 marcada como falha" test "$(cat "$DIR/FAILED_VERSION")" = build-4

echo "5) versão que falhou não é tentada de novo"
before=$(count restart)
run_updater >/dev/null
check "não reiniciou" test "$(count restart)" -eq "$before"

echo "6) release mais nova recupera"
publish build-6 NEW6
sleep 1.1
run_updater >/dev/null
check "binário trocado" test "$(payload)" = NEW6
check "VERSION atualizado" test "$(cat "$DIR/VERSION")" = build-6
check "marca de falha removida" test ! -e "$DIR/FAILED_VERSION"
check "backups limitados a 2" test "$(ls "$DIR/backups" | wc -l)" -eq 2

echo
if [ "$FAILS" -eq 0 ]; then echo "Todos os testes passaram."; else echo "$FAILS verificação(ões) falharam."; exit 1; fi
