# Benchmarks: baseline (0.3.0, c429bd7) × CoW (feat/cow-value-semantics)

**Data:** 2026-08-16 · Windows 11 · medições intercaladas (os dois binários
alternados dentro da mesma janela — rodadas sequenciais por rótulo mostraram
drift térmico de até ±10% nesta máquina e foram descartadas para o veredito;
ficam em `results/baseline.md` e `results/cow.md` como registro do harness).

**Checksums idênticos em todos os benchmarks** — as duas versões computam os
mesmos resultados.

## Tabela consolidada (mediana de 5 execuções intercaladas)

| bench | perfil | baseline_ms | cow_ms | delta | critério (spec §5.3) | veredito |
|---|---|---|---|---|---|---|
| bench_call_light | chamada O(1), array 10k por valor | 3412 | 2659 | **−22,1%** | ganho | ✅ |
| bench_call_readonly | leitor puro O(n), array 20k | 2223 | 2020 | **−9,1%** | melhora mensurável | ✅ |
| bench_spawn_sum | soma paralela via spawn_task | 1055 | 988 | −6,3% | neutro/ganho | ✅ |
| bench_conway | grid mutado via ref, 60 gerações | 2862 | 2817 | −1,6% | ≤ ~5% | ✅ |
| bench_map_churn | escrita intensa em map | 444 | 438 | −1,4% | ≤ ~5% | ✅ |
| bench_call_ref | mutação in-place via ref | 4495 | 4594 | +2,2% | neutro | ✅ |
| bench_bubblesort | sort in-place via ref | 4059 | 4173 | +2,8%¹ | ≤ ~5% | ✅ |
| bench_path_update | `a[i].x = v` em loop, dono único | 620 | 649 | +4,7% | ≤ ~5% | ✅ |
| bench_share_mutate | compartilha e muta em loop | 509 | 633 | **+24,3%** | livre, documentada | ✅² |

¹ Bubblesort tem a maior variância da suite (−2% a +7% entre sessões); o
valor reportado é a mediana de 9 execuções intercaladas dedicadas.

² Pior caso do CoW por construção: `let b = a` seguido de mutação paga um
clone O(n) por iteração — exatamente o custo que a semântica promete nesse
padrão. Migração: quem quer compartilhamento usa `ref` (custo zero, ver
`bench_call_ref`).

## Interpretação

**Onde o CoW ganha:** chamadas que só leem o composto deixam de pagar a cópia
rasa ansiosa O(n) por chamada. No caso assintótico (`call_light`: função O(1)
com array de 10k elementos), −22%; no leitor O(n) (`call_readonly`), −9%.

**Onde fica neutro:** mutação in-place via `ref` (`bubblesort`, `call_ref`,
`conway`) e contêineres de dono único (`map_churn`, `path_update`) pagam só o
branch de checagem `Shared` por escrita — dentro do ruído ou ≤5%.

**Onde paga:** o padrão compartilha-e-muta (`share_mutate`, +24%) — o preço
explícito da garantia de independência, com `ref` como válvula de escape.

**Achado colateral:** o ganho das chamadas só-leitura é limitado por um custo
pré-existente e independente do CoW — a validação de tipos em runtime varre
todos os elementos do array a cada chamada tipada
(`internal/vm/runtime_type_validation.go`, caso `TYPE_ARRAY`). Em
`call_light`, ela domina o tempo restante nas duas versões. Validar pela tag
`RuntimeType` em O(1) quando presente é a próxima otimização natural, fora do
escopo desta mudança.

## Reprodução

```powershell
# suite completa por binário (grava results/<label>.md)
powershell -File benchmarks/run_benchmarks.ps1 -Binary <exe> -Label <label>

# comparação intercalada (grava results/interleaved.md) — preferir esta
powershell -File benchmarks/interleaved_compare.ps1 -Baseline <exe> -Candidate <exe>

# corpus de exemplos baseline × candidato
powershell -File benchmarks/compare_examples.ps1 -Baseline <exe> -Candidate <exe>
```
