#!/usr/bin/env bash
# Port bash de run_cross_runtime.ps1 — mesmo protocolo, mesma saida.
#
# Compara o VM do Noxy com outros runtimes na mesma carga. Noxy e CPython
# cobrem os sete benches; Lua 5.4 e Go nativo cobrem tres (startup,
# loop_arith, fib) como calibracao. Runtime ausente vira "-" na tabela.
#
# Metodologia (ver README.md): intercalado, MINIMO de N amostras, fontes
# copiados para disco local, versao antiga medida na mesma janela
# (--noxy-baseline). Cada implementacao imprime a mesma linha CHECKSUM: e o
# script aborta se divergirem — nao seria a mesma carga.
#
# Uso:
#   ./run_cross_runtime.sh --noxy <bin> [--noxy-baseline <bin> --baseline-label v060]
#                          [--python python3] [--lua lua] [--runs 9]
set -euo pipefail
export LC_NUMERIC=C

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NOXY="$HERE/../../noxy"
NOXY_BASE=""
BASE_LABEL="noxy_base"
PYTHON="python3"
LUA="lua"
RUNS=9

while [ $# -gt 0 ]; do
    case "$1" in
        --noxy)           NOXY="$2"; shift 2 ;;
        --noxy-baseline)  NOXY_BASE="$2"; shift 2 ;;
        --baseline-label) BASE_LABEL="$2"; shift 2 ;;
        --python)         PYTHON="$2"; shift 2 ;;
        --lua)            LUA="$2"; shift 2 ;;
        --runs)           RUNS="$2"; shift 2 ;;
        -h|--help)        sed -n '2,15p' "$0"; exit 0 ;;
        *) echo "argumento desconhecido: $1" >&2; exit 2 ;;
    esac
done

have() { command -v "$1" >/dev/null 2>&1; }
[ -x "$NOXY" ] || have "$NOXY" || { echo "noxy nao encontrado: $NOXY" >&2; exit 2; }
have "$PYTHON" || { echo "python nao encontrado: $PYTHON" >&2; exit 2; }
HAS_LUA=0; have "$LUA" && HAS_LUA=1
HAS_GO=0;  have go && HAS_GO=1

WORK="$(mktemp -d "${TMPDIR:-/tmp}/noxy_cross_XXXXXXXX")"
trap 'rm -rf "$WORK"' EXIT
cp "$HERE"/*.nx "$HERE"/*.py "$WORK"/
cp "$HERE"/*.lua "$WORK"/ 2>/dev/null || true

# Go compila antes de medir: o benchmark e do binario, nao do compilador.
if [ "$HAS_GO" = 1 ]; then
    for d in "$HERE"/go/*/; do
        b="$(basename "$d")"
        go build -o "$WORK/go_$b" "$d/main.go" || { echo "go build falhou para $b" >&2; exit 1; }
    done
fi

# Ordem fixa: define a ordem das colunas e a ordem do intercalamento.
ORDER=(noxy)
[ -n "$NOXY_BASE" ] && ORDER+=("$BASE_LABEL")
ORDER+=(python lua go)

now_ms() { awk -v t="$EPOCHREALTIME" 'BEGIN{printf "%.3f", t*1000}'; }

run_one() { # $1=runtime $2=bench
    case "$1" in
        noxy)         "$NOXY" "$WORK/$2.nx" ;;
        "$BASE_LABEL") "$NOXY_BASE" "$WORK/$2.nx" ;;
        python)       "$PYTHON" "$WORK/$2.py" ;;
        lua)          "$LUA" "$WORK/$2.lua" ;;
        go)           "$WORK/go_$2" ;;
    esac
}

declare -A MIN CHK
BENCHES=()
for f in "$WORK"/*.nx; do BENCHES+=("$(basename "$f" .nx)"); done
IFS=$'\n' BENCHES=($(sort <<<"${BENCHES[*]}")); unset IFS

for b in "${BENCHES[@]}"; do
    # Monta so os runtimes que existem para este bench.
    rts=(noxy)
    [ -n "$NOXY_BASE" ] && rts+=("$BASE_LABEL")
    [ -f "$WORK/$b.py" ] && rts+=(python)
    [ "$HAS_LUA" = 1 ] && [ -f "$WORK/$b.lua" ] && rts+=(lua)
    [ -x "$WORK/go_$b" ] && rts+=(go)

    # Warmup (aquece o cache de arquivo) + equivalencia entre runtimes.
    chk=""
    for r in "${rts[@]}"; do
        c="$(run_one "$r" "$b" 2>&1 | grep -m1 '^CHECKSUM:' || true)"
        [ -n "$c" ] || { echo "$b/$r : sem linha CHECKSUM" >&2; exit 1; }
        [ -n "$chk" ] || chk="$c"
        [ "$c" = "$chk" ] || { echo "$b : checksum divergente ($r=$c, esperado $chk)" >&2; exit 1; }
    done
    CHK[$b]="$chk"

    declare -A samples=()
    for i in $(seq 1 "$RUNS"); do
        for r in "${rts[@]}"; do
            t0="$(now_ms)"; run_one "$r" "$b" >/dev/null 2>&1; t1="$(now_ms)"
            samples[$r]+=" $(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')"
        done
    done
    shown=""
    for r in "${rts[@]}"; do
        m="$(tr ' ' '\n' <<<"${samples[$r]}" | sed '/^$/d' | sort -n | head -1)"
        MIN[$b,$r]="$m"; shown+="  $r=${m}ms"
    done
    printf '%-12s%s\n' "$b" "$shown"
    unset samples
done

# ---- relatorio ----
PRESENT=()
for r in "${ORDER[@]}"; do
    for b in "${BENCHES[@]}"; do
        if [ -n "${MIN[$b,$r]:-}" ]; then PRESENT+=("$r"); break; fi
    done
done

cell()   { [ -n "${1:-}" ] && echo "$1" || echo "-"; }
# Liquido: total menos o piso de processo do proprio runtime. Para runtimes
# rapidos o trabalho cabe no ruido do piso e a subtracao vai a zero ou fica
# negativa — reportamos "~0" em vez de fingir precisao.
net()    { [ -n "${1:-}" ] && [ -n "${2:-}" ] || { echo "-"; return; }
           awk -v v="$1" -v f="$2" 'BEGIN{n=v-f; if (n<=5) print "~0"; else printf "%.1f", n}'; }
ratio()  { case "$1$2" in *~0*|*-*) echo "-"; return ;; esac
           awk -v a="$1" -v b="$2" 'BEGIN{printf "%.2fx", a/b}'; }
header() { local s="| bench |"; for r in "$@"; do s+=" $r |"; done; echo "$s"
           s="|---|"; for r in "$@"; do s+="---|"; done; echo "$s"; }

OUT="$HERE/results/cross_runtime.md"
mkdir -p "$HERE/results"
{
    echo "# Cross-runtime: Noxy x CPython x Lua x Go"
    echo
    echo "- noxy: \`$NOXY\` ($("$NOXY" --version 2>&1 | tr -d '\n'))"
    [ -n "$NOXY_BASE" ] && echo "- $BASE_LABEL: \`$NOXY_BASE\` ($("$NOXY_BASE" --version 2>&1 | tr -d '\n'))"
    echo "- python: $("$PYTHON" --version 2>&1 | tr -d '\n')"
    if [ "$HAS_LUA" = 1 ]; then echo "- lua: $("$LUA" -v 2>&1 | tr -d '\n')"; else echo "- lua: ausente"; fi
    if [ "$HAS_GO" = 1 ]; then echo "- go: $(go version)"; else echo "- go: ausente"; fi
    echo "- Sistema: $(uname -srm)"
    echo "- Data: $(date +%Y-%m-%dT%H:%M:%S)"
    echo "- Runs por bench: $RUNS, intercalados; **minimo** reportado"
    echo
    echo "## Tempo total (ms)"
    echo
    header "${PRESENT[@]}"
    for b in "${BENCHES[@]}"; do
        s="| \`$b\` |"; for r in "${PRESENT[@]}"; do s+=" $(cell "${MIN[$b,$r]:-}") |"; done; echo "$s"
    done
    echo
    echo "## Tempo de execucao, descontado o piso de \`startup\` (ms)"
    echo
    header "${PRESENT[@]}"
    for b in "${BENCHES[@]}"; do
        [ "$b" = startup ] && continue
        s="| \`$b\` |"; for r in "${PRESENT[@]}"; do s+=" $(net "${MIN[$b,$r]:-}" "${MIN[startup,$r]:-}") |"; done; echo "$s"
    done
    echo
    echo "\`~0\` = o trabalho cabe dentro do ruido do piso de processo do runtime."
    # Razoes sobre o liquido: os ms absolutos dependem da carga da maquina na
    # hora; a razao contra um runtime medido na MESMA janela, sim, se compara.
    OTHERS=(); for r in "${PRESENT[@]}"; do [ "$r" != noxy ] && OTHERS+=("$r"); done
    if [ "${#OTHERS[@]}" -gt 0 ]; then
        echo
        echo "## Razoes sobre o tempo liquido (noxy / outro)"
        echo
        labels=(); for r in "${OTHERS[@]}"; do labels+=("/ $r"); done
        header "${labels[@]}"
        for b in "${BENCHES[@]}"; do
            [ "$b" = startup ] && continue
            n="$(net "${MIN[$b,noxy]:-}" "${MIN[startup,noxy]:-}")"
            s="| \`$b\` |"
            for r in "${OTHERS[@]}"; do s+=" $(ratio "$n" "$(net "${MIN[$b,$r]:-}" "${MIN[startup,$r]:-}")") |"; done
            echo "$s"
        done
    fi
    echo
    echo "## Checksums"
    echo
    for b in "${BENCHES[@]}"; do echo "- \`$b\`: ${CHK[$b]}"; done
} > "$OUT"
echo "wrote results/cross_runtime.md"
