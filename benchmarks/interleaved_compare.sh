#!/usr/bin/env bash
# Port bash de interleaved_compare.ps1 — medicao intercalada de dois binarios
# na mesma janela de tempo, imune a drift termico/carga de background que
# contamina rodadas sequenciais por rotulo. Grava results/interleaved.md.
#
# O warmup tambem e o guard de equivalencia: cada bench imprime uma linha
# CHECKSUM: e os dois binarios tem de concordar. Bench que um dos binarios nem
# compila sai do erro em ~30ms e entraria na tabela como regressao de 10x —
# e pulado e listado, nao medido.
#
# Uso: ./interleaved_compare.sh --baseline <bin> --candidate <bin>
#          [--baseline-label baseline] [--candidate-label candidate] [--runs 5]
set -euo pipefail
export LC_NUMERIC=C

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE=""; CAND=""; BLABEL="baseline"; CLABEL="candidate"; RUNS=5
while [ $# -gt 0 ]; do
    case "$1" in
        --baseline)        BASE="$2"; shift 2 ;;
        --candidate)       CAND="$2"; shift 2 ;;
        --baseline-label)  BLABEL="$2"; shift 2 ;;
        --candidate-label) CLABEL="$2"; shift 2 ;;
        --runs)            RUNS="$2"; shift 2 ;;
        -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
        *) echo "argumento desconhecido: $1" >&2; exit 2 ;;
    esac
done
[ -n "$BASE" ] && [ -n "$CAND" ] || { echo "uso: $0 --baseline <bin> --candidate <bin> [...]" >&2; exit 2; }

now_ms()   { awk -v t="$EPOCHREALTIME" 'BEGIN{printf "%.3f", t*1000}'; }
median()   { sed '/^$/d' | sort -n | awk '{a[NR]=$1} END{print a[int((NR-1)/2)+1]}'; }
checksum() { "$1" "$2" 2>&1 | grep -m1 '^CHECKSUM:' || true; }
time_one() { local t0 t1; t0="$(now_ms)"; "$1" "$2" >/dev/null 2>&1 || true; t1="$(now_ms)"
             awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}'; }

lines=("| bench | ${BLABEL}_ms | ${CLABEL}_ms | delta |" "|---|---|---|---|")
skipped=()
for p in $(ls "$HERE"/bench_*.nx | sort); do
    name="$(basename "$p")"
    cb="$(checksum "$BASE" "$p")"; cc="$(checksum "$CAND" "$p")"
    if [ -z "$cb" ] || [ -z "$cc" ] || [ "$cb" != "$cc" ]; then
        if   [ -z "$cb" ]; then why="sem CHECKSUM no $BLABEL"
        elif [ -z "$cc" ]; then why="sem CHECKSUM no $CLABEL"
        else why="checksum divergente ($BLABEL=$cb, $CLABEL=$cc)"; fi
        skipped+=("$name — $why")
        echo "$name: PULADO ($why)"
        continue
    fi
    tb=""; tc=""
    for i in $(seq 1 "$RUNS"); do
        tb+="$(time_one "$BASE" "$p")"$'\n'
        tc+="$(time_one "$CAND" "$p")"$'\n'
    done
    mb="$(median <<<"$tb")"; mc="$(median <<<"$tc")"
    delta="$(awk -v a="$mb" -v b="$mc" 'BEGIN{printf "%.1f", 100*(b-a)/a}')"
    lines+=("| $name | $mb | $mc | $delta% |")
    echo "$name: $BLABEL=$mb $CLABEL=$mc delta=$delta%"
done
if [ "${#skipped[@]}" -gt 0 ]; then
    lines+=("" "Pulados (sem equivalencia entre os dois binarios):" "")
    for s in "${skipped[@]}"; do lines+=("- $s"); done
fi
mkdir -p "$HERE/results"
printf '%s\n' "${lines[@]}" > "$HERE/results/interleaved.md"
echo "wrote results/interleaved.md"
