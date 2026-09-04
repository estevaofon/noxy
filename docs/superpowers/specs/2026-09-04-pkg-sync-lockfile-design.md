# `noxy --sync`: lockfile completo e `noxy_libs` derivado

**Data:** 2026-09-04 · **Branch:** `feat/pkg-sync-lockfile`, a partir de `develop` (pós #137, v0.23.5)
**Status:** aprovado em conversa; revisada por revisor independente (achados 1–5 bloqueantes e 6–12 menores incorporados) · **Issue:** a abrir · **Relação:** spec de extensões por processo (`2026-08-29-process-extensions-design.md`, §8 e §15: "versioned `noxy.sum` keys" e "`noxy.sum` format spec" eram pendências declaradas); spec wasm (`2026-08-23`, §15 idem).

Um projeto clonado com `noxy.mod` e `noxy.sum` deve ficar pronto para rodar com **um comando**, `noxy --sync`, sem rede em tempo de execução e com o mesmo conteúdo em qualquer máquina. Hoje `--get` faz metade disso: clona, escreve `noxy.mod` e grava hash só de artefatos de extensão. Faltam quatro coisas, e cada uma tem precedente em Go, uv ou Cargo: hash para pacotes de fonte, registro do fechamento transitivo, versão sempre pinada e um comando que reconstrua `noxy_libs` a partir dos dois arquivos.

## 0. Fatos verificados antes do design (2026-09-04, `develop` em 12b22d4)

| O quê | Hoje |
|---|---|
| `noxy --get pkg[@ver]` (`internal/pkgmanager/manager.go`) | clone fresco em temp irmão, checkout, remove `.git`, `os.Rename` para `noxy_libs/<host_com>/<user>/<repo>`; grava `require` no `noxy.mod` raiz **só para o pacote pedido**; desce recursivamente nos `noxy.mod` das dependências com `visited` por `pkg@ver`: duas versões do mesmo pacote são **ambas** clonadas e a **última instalada vence** (`RemoveAll` + `Rename` do destino). Caso real: `--get quicksort@v0.1.0` instala v0.1.0, desce no auto-`require … HEAD` do próprio pacote (chave diferente) e substitui o diretório por `HEAD`, com o `noxy.mod` dizendo v0.1.0 |
| Sem versão | `HEAD`, exceto extensão por processo, que resolve a tag semver mais nova (`resolveNewestTag`, `git ls-remote --tags`) |
| `noxy.sum` (`sumfile.go`) | três campos `<github_com/user/repo> <arquivo> sha256:<hex>`; só manifesto + `.wasm` ou manifesto + `bin/<asset>` de **todas** as plataformas (hashes vindos de `checksums.txt`); pacote de fonte não tem linha |
| Chave do sum | caminho local (`github_com/...`), não o caminho do módulo do `noxy.mod` (`github.com/...`); a VM deriva a chave de `filepath.Rel(noxy_libs, dir)` em `verifyExtensionSum` (`internal/vm/extensions.go`) |
| Raiz | `--get` usa o **cwd** (`noxy.mod` e `noxy_libs` relativos); a VM usa `RootPath = filepath.Dir(script)` (`cmd/noxy/main.go`, `getDir`). Rodando `noxy noxy_examples/x.nx` da raiz do repositório, `RootPath` é `noxy_examples/`: o pacote é achado pelo candidato **relativo ao cwd** de `resolveModule`, e `verifyExtensionSum` cai no ramo "fora de `noxy_libs`" e **pula a verificação** dos exemplos de dynamodb (achado do revisor) |
| Verificação na carga | só extensões, só sob `<RootPath>/noxy_libs`; sem entrada → aviso TOFU `run 'noxy --get' to record it`; binário ausente → `binary bin/<asset> not found — run 'noxy --get' to download it` (`extensions.go:122`, testado em `process_extensions_e2e_test.go:129`) |
| `noxy.mod` (`modfile.go`) | `module`, `noxy <versão>`, `require <pkg> <ver>`; `Save` itera um `map` → **ordem não determinística** das linhas `require`; `--get` sobrescreve `noxy <versão>` com a versão do binário corrente |
| `noxy.mod` do repositório | `require github.com/estevaofon/quicksort v0.1.0` e `noxy_dynamodb v0.3.0`; o `noxy.mod` do quicksort **requer a si mesmo** em `HEAD`; `v0.1.0` é tag real no GitHub (`ls-remote`) |
| `noxy_libs` no git | `quicksort` e `math_lib` **commitados**; `noxy_terminal` e `noxy_dynamodb` ignorados por linha no `.gitignore`. `math_lib` não está no `noxy.mod` (biblioteca colocada à mão; `noxy_examples/test_libs.nx` a importa para exercitar a resolução por `noxy_libs`) |
| Resolução de `use` (`internal/vm/modules.go`) | `NOXY_PATH`, depois `<RootPath>/noxy_libs/<a/b/c>[.nx]`, `<RootPath>/stdlib`, `<RootPath>`, os mesmos três **relativos ao cwd**, embutido; falha com `module not found: <nome>` sem menção ao `noxy.mod` |
| CI (`.github/workflows/network-deadlines.yml`) | testes Go e o runner `noxy_examples/run_all_tests_concurrent.nx` em ubuntu e windows; `use_quicksort.nx` e `test_libs.nx` rodam no runner (não estão em `exclusions`), mas o `use` do `use_quicksort.nx` está **comentado** (quicksort inlined, "cross-module ref limitation"): o exemplo passa com `noxy_libs` vazio; os exemplos de dynamodb e space invaders estão excluídos |
| Costuras de teste (`release.go`) | `gitURLFor`, `releaseBaseURL`, `resolveNewestTag`, `httpClient` são variáveis trocáveis; os testes sobem repositório git local e `httptest` |

## 1. Objetivo, escopo e não-escopo

**Objetivo.** `git clone <projeto> && noxy --sync && noxy main.nx` funciona, offline após o sync, com bytes idênticos em qualquer SO, e `noxy --sync --locked` em CI falha se o lock não descreve exatamente o que o `noxy.mod` pede.

**Escopo.** Formato v2 do `noxy.sum` (lock com fechamento transitivo e hash de árvore); resolução de versão (tag mais nova, pseudo-versão, MVS); `--sync` e `--sync --locked`; `--get` como "adicionar/atualizar + sync"; mensagens de runtime que apontam para `--sync`; `noxy_libs` inteiro fora do git; passo de sync no CI; migração do sum v1.

**Fora de escopo** (continuações, §11): `replace <pkg> => ./caminho` para bibliotecas locais; cache global em `~/.cache/noxy`; subcomandos (`noxy get`, `noxy sync`) em vez de flags; registro central; assinaturas; hosts sem o layout `releases/download/<tag>/`.

## 2. Decisões e precedentes

| Decisão | Precedente | Alternativa descartada |
|---|---|---|
| `noxy.mod` é intenção (só diretas); `noxy.sum` é o lock (fechamento inteiro) | `pyproject.toml`/`uv.lock`; `Cargo.toml`/`Cargo.lock` | `// indirect` no `noxy.mod` (Go, exaustivo desde 1.17): polui o manifesto que o usuário edita à mão |
| Hash de árvore por pacote de fonte | `go.sum` `h1:` (dirhash `Hash1`) | hash por arquivo `.nx`: sum cresce com o pacote e a verificação vira N leituras |
| Versão sempre pinada; sem tag vira pseudo-versão | Go `v0.0.0-<data>-<sha12>` | `HEAD` no lock: não reproduz |
| Conflito de versões resolve pela maior (MVS) | Go MVS | última instalada vence (hoje): desfaz o pino do `noxy.mod` (§0) |
| Runtime nunca baixa; erro aponta o comando | Go `-mod=readonly` (default desde 1.16); Noxy "compilador fala primeiro", runtime offline (Lambda) | `uv run` sincroniza sozinho; Cargo baixa no build: rede em execução |
| `--sync` só apaga o que ele mesmo instalou (carimbo) | decisão própria: uv e Cargo apagam tudo que não está no lock (`uv sync` é exato por default) | apagar tudo que não está no lock: destruiria `math_lib` |
| `--locked` recusa mudar o lock | `cargo --locked`, `uv sync --locked` (`--frozen` do uv nem confere o manifesto), `GOFLAGS=-mod=readonly` | — |
| Raiz do projeto é o ancestral mais próximo com `noxy.mod` | Go (`go.mod`), Cargo (`Cargo.toml`), uv (`pyproject.toml`) | cwd para o sync e diretório do script para a VM (hoje): raízes diferentes, verificação pulada (§0) |
| Pseudo-versão baseada na tag anterior | Go `vX.Y.(Z+1)-0.<data>-<sha>` | `v0.0.0-…` sempre: perde o MVS para qualquer tag e desfaz um pino por sha |
| Verificação de fonte no sync; extensões continuam verificadas na carga | Go verifica no download, confia no cache | hash de toda a árvore a cada `noxy main.nx`: custo por execução |
| Flags (`--sync`), não subcomandos | CLI atual (`--get`, `--version`) | `noxy sync`: mudança separada de superfície |

## 3. Arquivos

### 3.0 Raiz do projeto

`pkgmanager.FindRoot(start string) (root string, ok bool)` sobe de `start` até encontrar um diretório com `noxy.mod`; é a **única** definição de raiz, usada pelos dois lados:

- `--sync`/`--get`: `start` é o cwd. Sem `noxy.mod` acima, `--sync` é erro `no noxy.mod in <cwd> or any parent`; `--get` cria um no cwd, como hoje.
- VM: `start` é `RootPath` (diretório do script). O resultado vai em `VMConfig.ProjectRoot` (vazio se não há `noxy.mod`), calculado uma vez em `NewWithConfig`. `resolveModule` ganha os candidatos `<ProjectRoot>/noxy_libs/...` **antes** dos de `RootPath`; `verifyExtensionSum` usa `<ProjectRoot>/noxy_libs` e `<ProjectRoot>/noxy.sum`; a dica de §6 lê `<ProjectRoot>/noxy.mod`. Os candidatos relativos ao cwd continuam existindo para o layout sem `noxy.mod`.

Efeito neste repositório: `noxy noxy_examples/dynamodb_example.nx` passa a verificar a extensão contra o `noxy.sum` da raiz, o que hoje é pulado (§0).

### 3.1 `noxy.mod`

Sintaxe inalterada. Semântica que muda:

- `require <pkg> <ver>` com `<ver>` ∈ {tag semver, pseudo-versão, `HEAD`}. `HEAD` significa "resolva na próxima resolução e **reescreva esta linha** com o resultado" (§4.1). Sob `--locked`, `HEAD` é erro.
- `noxy <versão>`: `--sync` compara com `version.Version`; binário **mais antigo** que o exigido é erro `noxy.mod requires noxy v0.24.0; this is v0.23.5` (precedente: diretiva `go` desde 1.21). A checagem vale para o `noxy.mod` de **toda** dependência do fechamento, não só do raiz (Go checa a diretiva `go` de cada módulo): a mensagem diz qual pacote exige. `--sync` nunca escreve essa linha; `--get` segue sobrescrevendo com a versão corrente, como hoje.
- `Save` ordena as linhas `require` por caminho do módulo (correção do fato §0: ordem não determinística). Comentários (`#`, `//`) continuam ignorados no parse e **perdidos** no save, como hoje.
- `require` de um pacote a si próprio (`module quicksort` + `require .../quicksort`) é ignorado na resolução, sem aviso: o `quicksort` publicado já faz isso, e hoje o auto-`require HEAD` **sobrescreve** a versão pinada (§0).
- Caminho de módulo é `host/user/repo` nu, sem esquema: `require https://github.com/x/y` e `--get git@github.com:x/y` são erro `module path must be host/user/repo`. Hoje `toGitURL` aceita as duas formas e `localPackagePath` produz um caminho quebrado para elas.

### 3.2 `noxy.sum` v2

Uma linha por fato, campos separados por espaço, ordenadas lexicograficamente no save, terminadas em `\n`:

```
<módulo> <versão> sha256:<hex>                 # hash de árvore do pacote (§3.3)
<módulo> <versão> <arquivo> sha256:<hex>       # artefato de extensão (manifesto, .wasm, bin/<asset>)
```

Exemplo para o `noxy.mod` deste repositório:

```
github.com/estevaofon/noxy_dynamodb v0.3.0 sha256:9f1c…
github.com/estevaofon/noxy_dynamodb v0.3.0 bin/noxy-plugin-dynamodb-darwin-amd64 sha256:ff4d…
github.com/estevaofon/noxy_dynamodb v0.3.0 bin/noxy-plugin-dynamodb-linux-amd64 sha256:69fe…
github.com/estevaofon/noxy_dynamodb v0.3.0 noxy_ext.toml sha256:bcca…
github.com/estevaofon/quicksort v0.1.0 sha256:a71e…
```

Regras:

- **Chave é o caminho do módulo** do `noxy.mod` (`github.com/...`), não o caminho local. A conversão local ↔ módulo fica num único par de funções em `pkgmanager` (`LocalPath(module)`, já existe como `localPackagePath`; `ModulePath(local)` nova: troca `_` por `.` no **primeiro** segmento — hostname não tem `_`), usadas pelo `--get`/`--sync` e pela VM. `math_lib`, sem host, vira a chave `math.lib`: inofensivo, nunca tem linha.
- **Invariante: uma versão por módulo.** MVS produz exatamente uma; `Save` recusa gravar duas. Versão é normalizada com prefixo `v` no parse do `noxy.mod` e do lock (`1.2.3` e `v1.2.3` são a mesma versão). A VM, que só conhece o diretório em disco, procura o artefato **por módulo e arquivo, ignorando a versão**; o invariante torna a busca unívoca.
- **O lock lista o fechamento transitivo inteiro**, diretas e indiretas, sem marcação. Diretas são as do `noxy.mod`; o resto é indireto por diferença.
- Linhas de artefato de uma extensão por processo continuam cobrindo **todas** as plataformas publicadas (hashes de `checksums.txt`), para o lock ser portável entre o macOS do colega e o Lambda. O hash de árvore **exclui** `bin/` (§3.3), então o mesmo lock verifica a fonte em qualquer plataforma.
- **Migração do v1** (três campos, chave local): o parser aceita as duas formas; linhas v1 são lidas como "sem hash de árvore" para o módulo correspondente e **descartadas** no próximo save: o `--sync` reinstala o pacote (§5.1 passo 3, sem hash não há `cached`) e regrava todas as linhas dele. Uma versão depois, o parser recusa v1. Quebra documentada no CHANGELOG.

### 3.3 Hash de árvore

Algoritmo do `dirhash.Hash1` do Go, com saída hex e prefixo `sha256:` para casar com as demais linhas:

1. Lista todos os arquivos regulares sob o diretório do pacote, caminhos relativos com `/`, ordenados por bytes.
2. Exclui `.git/` (já removido no clone), `bin/` (assets por plataforma, cobertos por linha própria) e `noxy_libs/` (um pacote não deve carregar dependências vendorizadas; se carregar, elas não contam).
3. Link simbólico é erro (`package contains a symlink: <caminho>`), como no Go.
4. Para cada arquivo, uma linha `"%x  %s\n"` com o sha256 do conteúdo e o caminho; o hash de árvore é o sha256 da concatenação das linhas.

Bytes do arquivo são os do repositório: o clone já usa `core.autocrlf=false`. Um pacote editado à mão em `noxy_libs` deixa de bater e o `--sync` seguinte o reinstala (é o comportamento desejado: `noxy_libs` é derivado).

### 3.4 Carimbo `noxy_libs/.noxy-sync`

Uma linha por pacote que o `--sync`/`--get` instalou: `<módulo> <versão>`. Só serve para a poda (§5.3): diretório presente no carimbo e ausente do lock novo é removido; diretório fora do carimbo (`math_lib`) nunca é tocado. Carimbo ausente ou corrompido é tratado como vazio (nada é podado, aviso em stderr). A linha de um pacote é acrescentada **logo após** a promoção do diretório (§5.1 passo 3), não no fim: um sync morto por `SIGKILL` deixa o carimbo cobrindo tudo que instalou. O arquivo é reescrito inteiro na poda (§5.3). Temporários `noxy_libs/.get-*` de um sync anterior morto são removidos no início de cada sync.

## 4. Resolução

### 4.1 Versão

| Pedido | Resolve para |
|---|---|
| `pkg@v1.2.3` | a própria tag, normalizada com `v` |
| `pkg` ou `pkg@HEAD` no **raiz** | tag semver mais nova (`git ls-remote --tags`, `newestSemverTag` já existente); sem tag semver → pseudo-versão do commit apontado por `HEAD` |
| `pkg@HEAD` no `noxy.mod` de uma **dependência** | a versão que o lock já tem para `pkg`, se tem (o `noxy.mod` da dependência está dentro do hash de árvore e não pode ser reescrito; sem esta regra todo sync iria à rede e `--locked` falharia no dia em que `pkg` publicasse uma tag). Fora do lock, como no raiz; sob `--locked`, erro |
| `pkg@<sha>` ou `pkg@<branch>` | pseudo-versão do commit resolvido |

Pseudo-versão, forma do Go: se uma tag semver `vX.Y.Z` é ancestral do commit (`git describe --tags --match 'v[0-9]*' --abbrev=0`), `vX.Y.(Z+1)-0.<yyyymmddhhmmss UTC do commit>-<12 hex do sha>`; senão `v0.0.0-<data>-<sha>`. Com isso um `pkg@<sha>` posterior à v0.1.0 vence uma `require v0.1.0` no MVS, em vez de perder o pino explícito do usuário. Data e sha vêm de `git log -1 --format=%ct %H` no clone temporário. Clone de pseudo-versão faz `git checkout <sha12>`. Extensão por processo continua exigindo tag (assets pendem de release): pseudo-versão para `kind = "process"` é erro, mensagem de hoje.

Ordem entre versões é a do semver: pseudo-versão é pré-release da versão seguinte à tag base, então fica acima da base e abaixo da tag seguinte; duas pseudo-versões com a mesma base comparam pelo timestamp. Isso substitui o "tag contra pseudo-versão é erro" da conversa: a ordem semver já decide, sem caso especial.

### 4.2 Fechamento e MVS

Entrada: as `require` do `noxy.mod` raiz (com `HEAD` já resolvido por §4.1) e o lock atual. O fechamento é **sempre** recalculado a partir das diretas; o lock entra como pino, não como atalho (é o modelo do Cargo: `Cargo.lock` é preferência, o manifesto é a verdade). Algoritmo:

1. Fila com as diretas. Para cada `(pkg, ver)` tirado da fila, obtém o `noxy.mod` do pacote nessa versão (§4.3) e enfileira suas `require`, resolvendo `HEAD` por §4.1 e ignorando auto-`require`.
2. Acumula, por módulo, o **conjunto** de versões exigidas. Escolhe a **maior** de cada (MVS). Para módulo **indireto** que já está no lock, a versão do lock entra no conjunto como mais um requisito: o lock é piso e não deixa uma indireta descer em silêncio (Cargo). Para módulo **direto**, o `noxy.mod` manda: a versão do lock é ignorada na escolha.
3. Se a escolha de algum módulo mudou depois que seu `noxy.mod` foi lido, relê na versão escolhida e volta ao passo 1 até ponto fixo. Grafos de dependência de Noxy são pequenos; não há otimização.
4. Ciclos: `visited` por `(pkg, ver)`, como hoje.

Resultado: `closure = map[módulo]versão`. Módulo do lock que não foi alcançado sai do lock e é podado (§5.3). Num projeto já sincronizado o passo 1 lê todo `noxy.mod` do disco (§4.3) e nenhuma rede é tocada: `HEAD` já foi pinado e toda versão pedida já está instalada.

Mensagem quando a escolha difere do que o raiz pediu diretamente: `github.com/x/y: noxy.mod requires v1.0.0, but github.com/a/b requires v1.2.0; using v1.2.0` (informativo, stdout). O `noxy.mod` raiz **não** é reescrito nesse caso (Go também não): a linha direta segue como piso, e o lock registra a escolhida.

### 4.3 De onde vem o `noxy.mod` de uma dependência

- Se `noxy_libs/<local>` existe, o carimbo diz que está na versão pedida e o hash de árvore bate com o lock: lê do disco. Sem rede.
- Caso contrário: clone fresco em `noxy_libs/.get-<repo>-*` (temp irmão, como hoje), checkout, `.git` removido. O clone temporário fica vivo até o fim do sync, indexado por `(pkg, ver)`, e é **promovido** por `os.Rename` se essa versão for a escolhida; senão é apagado. Nenhum pacote é clonado duas vezes na mesma versão.

## 5. Comandos

### 5.1 `noxy --sync`

1. Lê `noxy.mod` (erro se ausente: `no noxy.mod in <dir>`) e `noxy.sum` (ausente = vazio; linhas v1 contam como "sem hash de árvore"). Checa `noxy <versão>` (§3.1).
2. Calcula o `closure` (§4.2). Linhas de artefato de pacotes cuja versão não mudou são preservadas do lock antigo (os hashes das outras plataformas vieram de `checksums.txt`, que não é rebaixado).
3. Para cada `(pkg, ver)` do `closure`, em ordem lexicográfica:
   - diretório existe, carimbo diz `ver`, o lock tem hash de árvore e ele bate → `cached`;
   - senão instala: clone (ou o temporário de §4.3), hash de árvore calculado, comparado com o lock se o lock tinha entrada (mismatch é erro fatal: `github.com/x/y v1.0.0: tree hash mismatch — noxy.sum has sha256:…, download has sha256:…`), gravado se não tinha (TOFU, como hoje); extensão por processo baixa `checksums.txt` e o asset desta plataforma (`fetchProcessBinaries`, existente), verificando contra o lock quando há linha; extensão wasm grava manifesto + `.wasm`.
   - `os.RemoveAll` do destino, `os.Rename` do temporário, como hoje.
4. Poda (§5.3), reescreve carimbo, `noxy.sum` e, se alguma linha `HEAD` foi pinada, `noxy.mod`.
5. Saída, uma linha por pacote, prefixo alinhado:
   ```
   Resolved 2 packages
   github.com/estevaofon/quicksort v0.1.0        cached
   github.com/estevaofon/noxy_dynamodb v0.3.0    installed (bin/noxy-plugin-dynamodb-linux-amd64)
   Removed github.com/old/dep v0.9.0
   Done.
   ```
   Progresso em stdout como hoje (é o "programa" `--sync`); erros em `diagOut` pela CLI, exit 1.

Um `--sync` num projeto já sincronizado não toca a rede e não reescreve arquivo cujo conteúdo não mudou. Sync que falha no meio (rede no terceiro pacote) deixa os já instalados em disco e no carimbo, mas sem hash no lock: o próximo sync os reinstala. Custa um clone, não corrompe nada. Dois `--sync` concorrentes no mesmo projeto não são protegidos por lock de arquivo (não-escopo).

### 5.2 `noxy --sync --locked`

Igual a §5.1, com recusas antes de qualquer escrita: `HEAD` no `noxy.mod` → `noxy.mod pins github.com/x/y to HEAD; run 'noxy --sync' to resolve it`; `closure` calculado diferente do lock (módulo a mais, a menos, versão diferente ou sem hash de árvore) → `noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked`. Um pacote **ausente do disco** mas presente no lock é instalado normalmente: `--locked` congela os arquivos, não o disco. Hash divergente no download é erro como sempre. Flag booleana `--locked`, válida só junto de `--sync` (erro caso contrário).

### 5.3 Poda

Para cada linha `<módulo> <ver>` do carimbo cujo módulo não está no `closure`: `os.RemoveAll(noxy_libs/<local>)`, linha `Removed …`. Diretórios-pai vazios (`noxy_libs/github_com/estevaofon/`) são removidos até `noxy_libs`. Linhas do `noxy.sum` de módulos fora do `closure` são descartadas no save. Nada fora do carimbo é tocado.

### 5.4 `noxy --get pkg[@ver]`

Passa a ser "adicionar ou atualizar": resolve a versão (§4.1), grava `require` no `noxy.mod` raiz (substituindo a linha se já existia) e chama o sync de §5.1. **Não** toca o lock: se a versão resolvida é a que o lock já tem, o hash existente vale e um download divergente (tag movida) é o erro fatal de sempre; só versão diferente troca as linhas do módulo, no sync. Sem isso, o comando que o usuário roda para "reinstalar" aceitaria uma tag movida por TOFU. Tudo que `--get` fazia por conta própria (descida recursiva, gravação de sum, mensagens de capabilities) passa a acontecer dentro do sync; `--get` vira ~20 linhas. `--get` sem versão num pacote já pinado **atualiza** para a tag mais nova (é `go get pkg@latest`; quem quer só instalar usa `--sync`).

Remover dependência: apagar a linha do `noxy.mod` e rodar `--sync`; a poda faz o resto.

## 6. Runtime

- `resolveModule` (`modules.go`), no ramo `module not found`: se existe `<RootPath>/noxy.mod` e alguma `require` dele tem caminho local que é prefixo do nome pedido (com `.` → `/`), a mensagem vira `module not found: github_com.estevaofon.quicksort (required by noxy.mod) — run 'noxy --sync'`. Sem `noxy.mod`, mensagem de hoje. A leitura do `noxy.mod` acontece só no caminho de erro: zero custo no caminho feliz. Continua em `modules.go` (guarda de arquitetura).
- `extensions.go:122`: `binary bin/<asset> not found — run 'noxy --sync' to download it`; aviso TOFU idem (`run 'noxy --sync' to record it`). O teste e2e que fixa a string muda junto.
- `verifyExtensionSum`: chave passa a ser `ModulePath(rel)`; `Lookup(módulo, arquivo)` ignora versão (§3.2). Nada mais muda: fonte de pacote não é verificada na carga.

## 7. Repositório e CI

- `.gitignore`: as duas linhas por extensão viram `noxy_libs/*` seguido de `!noxy_libs/math_lib/`. `math_lib` fica como fixture da resolução por `noxy_libs` (`test_libs.nx`) até existir `replace` (§11). `quicksort` sai do git (`git rm -r --cached`); passa a vir do `--sync`.
- `noxy.sum` do repositório é regravado no formato v2 pelo próprio `--sync` e commitado; `noxy.mod` ganha as linhas ordenadas.
- CI: nos jobs que rodam o runner de exemplos, um passo `go run ./cmd/noxy --sync --locked` antes do runner. O job passa a precisar de rede para `github.com/estevaofon/quicksort@v0.1.0`; é o mesmo que `go mod download` já faz no `actions/setup-go`. `use_quicksort.nx` tem o `use` **comentado** e não prova nada (§0); a aceitação é um exemplo novo `noxy_examples/use_quicksort_pkg.nx` que faz `use github_com.estevaofon.quicksort.quicksort select *` de verdade (o pacote expõe `quicksort.nx`; a limitação de referência cruzada citada no comentário precisa ser reconferida no plano — se persistir, o exemplo usa `select` de uma função sem `ref`), rodando no runner em ubuntu e windows após o `--sync --locked`.
- `docs/PACKAGE_MANAGER.md` reescrito: fluxo `--get` / `--sync` / `--locked`, formato v2, carimbo, o que commitar (`noxy.mod` e `noxy.sum` sim, `noxy_libs` não). `AGENTS.md` ganha uma linha na tabela de pacotes e o passo de sync na verificação obrigatória.

## 8. Estrutura de código

Tudo em `internal/pkgmanager`, arquivos por tema como o compilador:

| Arquivo | Responsabilidade |
|---|---|
| `modfile.go` | + `Save` ordenado; + `SelfRequire` ignorado no parse ou na resolução |
| `sumfile.go` | v2: `Entry{Module, Version, File, Digest}`; parse v1+v2; `Save` ordenado, invariante uma versão por módulo; `Lookup(module, file)` sem versão; `TreeHash(module)`; `ModulePath`/`LocalPath` |
| `dirhash.go` (novo) | §3.3 |
| `semver.go` (novo) | parse, comparação (tags e pseudo-versões), `PseudoVersion(ts, sha)`; `semverTagRE` migra de `release.go` |
| `resolve.go` (novo) | §4: `resolveVersion`, `closure` (MVS), cache de clones temporários |
| `sync.go` (novo) | §5.1–5.3: `Sync(root string, opts SyncOptions) error` com `root` de `FindRoot(cwd)`, carimbo, poda, saída |
| `root.go` (novo) | `FindRoot(start) (string, bool)` (§3.0); ausência de `noxy.mod` é caso normal para VM e compilador, o erro é do `--sync` |
| `manager.go` | `Get` reduzido a §5.4; `gitClone`/`gitCheckout`/`readManifest` ficam |
| `release.go` | inalterado além da migração do regex |
| `cmd/noxy/main.go` | flags `--sync` (bool) e `--locked` (bool); erro de `--locked` sem `--sync` |
| `internal/vm/vm.go`, `modules.go`, `extensions.go` | `ProjectRoot` (§3.0), §6 |

Funções de git e rede continuam atrás das costuras existentes (`gitURLFor`, `resolveNewestTag`, `httpClient`); uma nova, `gitCommitInfo` (data, sha, tag base), para a pseudo-versão.

## 9. Testes

- **`dirhash_test.go`**: golden de um diretório fixo (ordem, exclusão de `bin/` e `noxy_libs/`, symlink é erro, CRLF muda o hash).
- **`semver_test.go`**: ordem tag/pseudo/pseudo; formatação da pseudo-versão.
- **`sumfile_test.go`**: parse v1 e v2, save ordenado, recusa de duas versões, `Lookup` sem versão, `ModulePath(LocalPath(m)) == m`.
- **`resolve_test.go`**: repositórios git locais (helper `gitIn` existente) formando um grafo `raiz → a@v1, b@v1; b → a@v2` → escolhe `a@v2` e imprime o aviso; auto-`require HEAD` **não** sobrescreve a versão pedida (regressão do §0); `HEAD` no raiz sem tag vira pseudo-versão e é gravada no `noxy.mod`; `HEAD` numa dependência usa a versão do lock sem chamar `resolveNewestTag`; pseudo-versão com tag base vence a tag no MVS; pacote sem `noxy.mod` é folha; `require https://…` é erro.
- **`sync_test.go`**: (1) projeto sincronizado não chama `resolveNewestTag` nem `gitURLFor` (costuras que falham se chamadas); (1b) `require` removido do `noxy.mod` some do lock e do disco; (2) diretório apagado é reinstalado com hash igual; (3) diretório editado à mão é reinstalado; (4) hash de download diferente do lock é erro e não toca `noxy_libs`; (5) `--locked` recusa `HEAD`, `require` novo, versão mudada e linha v1; (6) poda remove o que está no carimbo e não o que está fora; (7) migração v1 → v2 regrava o pacote; (8) segundo sync não reescreve arquivos (mtime); (9) `--get pkg@vX` com `vX` já no lock e download divergente é erro; (10) sync morto após um pacote (simulado por costura que falha no segundo) deixa o carimbo com o primeiro e `.get-*` é limpo no sync seguinte; (11) `FindRoot` a partir de subdiretório.
- **`manager_get_test.go`**: os testes existentes de extensão por processo passam a exercitar `Get → Sync`; adicionar "`--get` num pacote já pinado atualiza".
- **VM**: `modules_test` para a dica `required by noxy.mod` e para `use` resolvido via `ProjectRoot` a partir de um script em subdiretório; teste de que `verifyExtensionSum` verifica (não pula) um script em `noxy_examples/` com `noxy.sum` na raiz; `process_extensions_e2e_test` com a string nova; teste de `verifyExtensionSum` com chave por módulo.
- **Aceitação**: `use_quicksort.nx` no runner em CI após `--sync --locked`; `noxy_terminal` e `noxy_dynamodb` reinstalados com `--sync` localmente e um exemplo de cada executado (manual, como na spec de processo).

## 10. Migração e CHANGELOG

Versão `v0.24.0` (quebra de formato). CHANGELOG, seção **Changed (BREAKING)**:

- `noxy.sum` v2: chave por caminho do módulo e versão; linhas v1 aceitas por uma versão e regravadas no primeiro `--sync`.
- `noxy_libs` é derivado: não commitar; `noxy --sync` reconstrói. Quem tinha `noxy_libs` commitado roda `--sync` uma vez e faz `git rm -r --cached noxy_libs`.
- `--get` sem versão resolve a tag mais nova (antes `HEAD`) para qualquer pacote, não só extensão por processo; `HEAD` no `noxy.mod` é pinado no próximo `--sync`.
- Mensagens `run 'noxy --get'` viram `run 'noxy --sync'`.

- Caminho de módulo só na forma `host/user/repo`; `https://` e `git@` são erro.

**Added**: `--sync`, `--locked`, hash de árvore, MVS, pseudo-versão, carimbo, raiz do projeto pelo `noxy.mod` mais próximo (a VM passa a verificar extensões de scripts em subdiretórios), dica no `module not found`.

## 11. Continuações

- `replace <pkg> => ./caminho` no `noxy.mod` (Go): tira `math_lib` da exceção do `.gitignore` e dá caminho a bibliotecas em desenvolvimento.
- Cache global `~/.cache/noxy/<módulo>@<versão>` com cópia para `noxy_libs` (uv): elimina o clone repetido entre projetos.
- Subcomandos `noxy get|sync|tidy`.
- `noxy --sync --upgrade` (`uv lock --upgrade`, `go get -u`): sobe todas as diretas para a tag mais nova.
- Verificação opcional do hash de árvore na carga (`NOXY_VERIFY=1`) para ambientes onde `noxy_libs` não é confiável.
