#!/usr/bin/env bash
# Port bash de run_benchmarks.ps1 — suite completa por binario, mediana de N,
# grava results/<label>.md. Bench sem linha CHECKSUM (nao compila neste
# binario, ou nunca teve) e pulado e listado no fim, nao medido. Para comparar
# dois binarios prefira interleaved_compare.sh (a unica comparacao que vale).
#
# Uso: ./run_benchmarks.sh --binary <bin> --label <label> [--runs 5]
set -euo pipefail
export LC_NUMERIC=C

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY=""; LABEL=""; RUNS=5
while [ $# -gt 0 ]; do
    case "$1" in
        --binary) BINARY="$2"; shift 2 ;;
        --label)  LABEL="$2"; shift 2 ;;
        --runs)   RUNS="$2"; shift 2 ;;
        -h|--help) sed -n '2,6p' "$0"; exit 0 ;;
        *) echo "argumento desconhecido: $1" >&2; exit 2 ;;
    esac
done
[ -n "$BINARY" ] && [ -n "$LABEL" ] || { echo "uso: $0 --binary <bin> --label <label> [--runs N]" >&2; exit 2; }

now_ms() { awk -v t="$EPOCHREALTIME" 'BEGIN{printf "%.3f", t*1000}'; }
median() { sed '/^$/d' | sort -n | awk '{a[NR]=$1} END{print a[int((NR-1)/2)+1]}'; }

OUT_DIR="$HERE/results"; mkdir -p "$OUT_DIR"
OUT="$OUT_DIR/$LABEL.md"
{
    echo "# Benchmark results: $LABEL"
    echo
    echo "- Binary: \`$BINARY\`"
    echo "- Date: $(date +%Y-%m-%dT%H:%M:%S)"
    echo "- Runs per bench: $RUNS (median reported)"
    echo
    echo "| bench | median_ms | runs_ms | checksum |"
    echo "|---|---|---|---|"
} > "$OUT"

skipped=()
for p in $(ls "$HERE"/bench_*.nx | sort); do
    name="$(basename "$p")"
    chk="$("$BINARY" "$p" 2>&1 | grep '^CHECKSUM:' | paste -sd';' || true)"
    if [ -z "$chk" ]; then
        skipped+=("$name — $("$BINARY" "$p" 2>&1 | head -1 || true)")
        echo "$name: PULADO (sem linha CHECKSUM)"
        continue
    fi
    times=""
    for i in $(seq 1 "$RUNS"); do
        t0="$(now_ms)"; "$BINARY" "$p" >/dev/null 2>&1; t1="$(now_ms)"
        times+="$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}') "
    done
    times="${times% }"
    med="$(tr ' ' '\n' <<<"$times" | median)"
    echo "| $name | $med | $times | $chk |" >> "$OUT"
    echo "$name: median=${med}ms checksum=$chk"
done
if [ "${#skipped[@]}" -gt 0 ]; then
    { echo; echo "Pulados (sem linha CHECKSUM):"; echo; for s in "${skipped[@]}"; do echo "- $s"; done; } >> "$OUT"
fi
echo "wrote results/$LABEL.md"
