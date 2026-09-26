# `noxy build`: executável autocontido a partir de um programa Noxy

**Data:** 2026-09-26 · **Branch:** `develop` (v0.26.0, ainda sem tag)
**Status:** aprovado, a implementar na v0.26.0 · **Origem:** distribuir o Noxy-Editor (`noxy editor.nx [pasta | arquivo]`) como `noxy-editor` para máquinas sem `noxy` nem `noxy_libs` · **Relação:** package manager (`docs/PACKAGE_MANAGER.md`, spec 2026-09-04), extensões por processo (`2026-08-29-process-extensions-design.md`), módulo `process` (`2026-09-26-process-module-design.md`).

`noxy build editor.nx -o dist/noxy-editor` copia o próprio `noxy`, anexa ao fim um payload com o programa, seus módulos, os plugins da plataforma e os assets declarados, e fecha com um trailer. O executável gerado, ao iniciar, encontra o payload, extrai-o uma vez para o cache do usuário e roda o programa como se fosse `noxy editor.nx <args>`. É o modelo do `deno compile` e do Node SEA, na variante mais simples: bytes anexados, não segmento do formato executável.

## 0. Fatos verificados antes do design (2026-09-26, `develop` 7eca173)

| O quê | Hoje |
|---|---|
| Resolução de `use` | Duplicada: a VM em `internal/vm/modules.go` (`resolveModule`, runtime) e o compilador em `internal/compiler/module_exports.go` (`moduleFileCandidates`/`resolveModuleDeclarations`, descoberta de tipos). Mesma ordem de candidatos: `NOXY_PATH` (3 formas) → `<projeto>/noxy_libs` → `<raiz>/noxy_libs` → `<raiz>/stdlib` → `<raiz>/<nome>` → os mesmos relativos ao cwd → `stdlib.FS` embutida. Arquivo antes de diretório; diretório com `<base>.nx` ou `main.nx` vira módulo de arquivo; senão cada `.nx` e subdiretório é submódulo. Leitura direta com `os.Stat`/`os.ReadFile`/`os.ReadDir` nos dois lugares |
| Quem compila os módulos | A VM, em runtime, no `use` (`compileAndRunModule`): compila com `SetKnownGlobals(vm.GlobalNames())` **depois** de carregar a extensão ao lado (`ensureExtensionLoaded`) e executa o corpo. O compilador só parseia e valida (`parseModuleDeclarations` → `validator.Compile` sem known globals) para descobrir exports e structs. Um erro de compilação num módulo só aparece quando o programa roda |
| Extensões (`internal/vm/extensions.go`) | `ensureExtensionLoaded(dir)` lê `dir/noxy_ext.toml` e `dir/bin/<asset>` (ou `.wasm`) do disco; `kind = "process"` recebe um caminho **absoluto e real** (`ext.NewProcess`), sem subir o processo. `verifyExtensionSum` só se aplica a `dir` sob `<raiz>/noxy_libs` e confere contra `<raiz>/noxy.sum` (`ParseSumFile` de arquivo ausente devolve lock vazio, sem erro); sem entrada é TOFU com aviso |
| Raiz do projeto | `pkgmanager.FindRoot(dir)`: ancestral mais próximo com `noxy.mod`; a VM parte do diretório do script (`VMConfig.RootPath`) e guarda em `ProjectRoot` |
| `noxy.mod` (`internal/pkgmanager/modfile.go`) | Linhas `module`, `noxy`, `require`; diretiva desconhecida é **ignorada** pelo parser; `Save` reescreve o arquivo só com as três (chamado por `--get`) |
| Releases | O noxy no GitHub **não publica binários** (v0.25.1: `assets: []`; instalação por `go install`). Plugins publicam `noxy-plugin-<nome>-<goos>-<goarch>[.exe]` + `checksums.txt`; `noxy.sum` guarda o hash de **todos** os assets; `ReleaseBaseURL`/`downloadAsset` derivam a URL e baixam com verificação |
| CLI (`cmd/noxy/main.go`) | `flag` com `--flags` e um positional; sem subcomando; REPL sem positional; `runFile(filename, content, rootPath, ...)` → `runWithConfig` instala tratador de sinais, `defer CloseExtensions`/`CloseProcesses`; `sys_argv` devolve `os.Args` cru |
| Serialização de bytecode | Não existe (nenhum gob/Marshal em `chunk`, `compiler`, `value`) |
| Monomorfização | O corpo do template é clonado do AST do módulo para o chunk do importador (`instanceQueue`, `substituteFunction/substituteStruct`), com nome qualificado `mod::Base<T>` idêntico entre módulo e importador (`isInstanceName`) |
| Noxy-Editor | `editor.nx` acha `web/` por `dirname(argv[1]) + "/web"`; `src/server.nx` entrega os arquivos por `serve_static(caminho real)`; `src/runner.nx` roda o F5 com `noxy <arquivo>` literal pelo shell; plugins `noxy_webview` e `noxy_pty` são processos. Não existe `sys.executable()` |
| Tamanhos (Linux, sem strip) | `noxy` 23,8 MB; `web/` do editor 568 KB; `noxy-plugin-webview-linux-amd64` 2,1 MB; `noxy-plugin-pty-linux-amd64` 2,0 MB |
| Testes existentes reaproveitáveis | `cmd/noxy/sync_flags_test.go` compila o binário com `go build` e o executa; `internal/vm/process_extensions_e2e_test.go` monta `noxy_libs/guest` com o guest de `internal/ext/exttest.BuildProcessGuest`; `noxy_libs/math_lib` é fixture fixa do repositório (`noxy_examples/test_libs.nx`); a CI roda `go test ./cmd/...` e `./internal/...` em Ubuntu e Windows |

## 1. Objetivo, escopo e não-escopo

**Objetivo.** Um comando `noxy build <entry.nx>` que produz um executável único, e um modo aplicação no `noxy` que o executa sem `noxy`, `noxy_libs` ou fonte na máquina de destino. Critério de aceite: `noxy build editor.nx -o dist/noxy-editor` no Noxy-Editor e `dist/noxy-editor ../qualquer-pasta` abre o editor com janela (webview), terminal (pty) e F5, sem `noxy` nem `noxy_libs` presentes.

**Escopo.**
- `noxy build` com `-o`, `--include` (repetível) e `--list`; diretiva `include` no `noxy.mod`.
- Formato do payload (zip + manifesto `noxy-app.json`) e do trailer `NOXYAPP1`; pacote `internal/bundle`.
- Modo aplicação em `cmd/noxy`: detecção, extração atômica para o cache do usuário, `argv`, execução; variável `NOXY_INTERPRETER`; variável `NOXY_APP_CACHE`.
- Interface única de origem de módulos `internal/modsrc.Source` com uma implementação (`DiskSource`), usada pelo compilador e pela VM no lugar das duas resoluções de hoje.
- Pipeline de build em `internal/build`: grafo de `use`, checagem de compilação de cada módulo **no build**, extensões da plataforma, includes.
- Builtin `sys.executable()`.
- Testes unitários (formato, trailer, extração, resolução, plano) e de integração (`go test ./cmd/...`, Ubuntu e Windows na CI existente); `docs/BUILD.md`, README, CHANGELOG (dentro da 0.26.0, sem bump), spec §12, AGENTS.md.
- Fecho no Noxy-Editor: `include web` no `noxy.mod` e o runner do F5 com `sys.executable()` + `NOXY_INTERPRETER=1` no processo filho.

**Fora de escopo** (v2 e seguintes, §12): bytecode no payload e leitura de módulos de dentro do zip (`PayloadSource`); `--target`/cross-compile (entra quando a release do noxy publicar `noxy-<goos>-<goarch>` com `checksums.txt`, sem muleta de `--runtime`); `codesign` no macOS (macOS é experimental na v1); módulo `assets` read-only; limpeza de caches antigos; `sys_load_plugin` (sai na v0.27.0; o build recusa); proteção do código-fonte.

## 2. Decisões e precedentes

| Decisão | Precedente | Alternativa descartada |
|---|---|---|
| Fonte no payload (`kind: "source"`), não bytecode | Não há serializador de Chunk; a monomorfização exige o AST do template do módulo no compilador do importador, então bytecode por módulo isolado não basta. Com o programa fechado, a v2 pode compilar tudo no build e serializar os chunks já instanciados, dispensando templates em runtime | Bytecode agora: serializador de Chunk com constantes aninhadas + trocar `compileAndRunModule` por "executar pré-compilado" — passo seguinte, não o primeiro |
| Uma origem só: o projeto inteiro é extraído para `<appdir>` e a `DiskSource` resolve a partir dali, **selada** (sem `NOXY_PATH`, sem candidatos relativos ao cwd) | Com `noxy.mod` e `noxy.sum` extraídos junto, `FindRoot` e `verifyExtensionSum` funcionam sem caminho especial; os plugins exigem arquivo real de qualquer forma | `PayloadSource` lendo do zip: segunda implementação para ganhar só a não-extração do fonte, que a v2 (bytecode) muda de novo |
| Bytes anexados + trailer fixo no fim, como o `deno compile` original | Simples, portátil, independente do formato executável; leitura do trailer custa um `open` + `read` de 64 bytes | Segmento/seção do formato (Mach-O, PE, ELF), como Node SEA/`postject` e o `sui` do Deno atual: é o plano B para o macOS (§11) |
| Payload é um zip (`archive/zip`, deflate) com manifesto JSON | Formato inspecionável com `unzip -l`; compressão por arquivo; CRC32; central directory no fim casa com o modelo de trailer | Formato próprio (tar-like): sem ferramenta de inspeção, sem ganho |
| Assets por `--include` **e** `include` no `noxy.mod`, união dos dois, relativos à raiz do projeto | `noxy.mod` é a intenção do projeto (`include web` fica versionado); a flag cobre o caso avulso | Só flag (o editor precisaria de script de build) ou só `noxy.mod` (script solto sem `noxy.mod` não teria como declarar) |
| Extração para `<cache>/noxy/apps/<sha256[:16]>/`, hash conferido **só na extração**, presença do marcador basta nos starts seguintes | Wazero já usa `<cache>/noxy/wazero`; conferir 26 MB a cada start seria custo fixo sem ganho (o cache é do usuário) | FS virtual no `io`: plugins são processos e só leem arquivos reais; `serve_static`, `process.start` e todo builtin de `io` teriam de ser interceptados |
| `argv` = `[exe, <appdir>/<entry>, args...]`; cwd **não muda** | "Exatamente como `noxy editor.nx <args>`": `dirname(argv[1])` continua sendo a raiz do programa; o editor abre a pasta do cwd | Chamar `chdir(appdir)`: quebraria `noxy-editor .` |
| `NOXY_INTERPRETER=1` faz o binário ignorar o payload e virar o `noxy` comum | O F5 do editor precisa de um interpretador; a variável é herdada, e o runner a põe **só no filho** | Flag na linha de comando: colidiria com os args do programa embutido |
| `sys.executable() -> string` (`os.Executable`, `""` em falha) | `sys.getcwd()` devolve `""` em falha; Python `sys.executable`, Node `process.execPath` | `PathResult`: `os.Executable` só falha sem `/proc`; não vale um struct |
| Erro de compilação sai no build: cada módulo alcançável é compilado (sem executar) com os nativos conhecidos, na ordem em que a VM o faria | Requisito explícito; a VM já tem o compilador de módulo, só falta separar "compilar" de "executar" | Confiar na validação do compilador (`parseModuleDeclarations`): roda sem known globals e some com a mensagem |
| `noxy build` é subcomando (primeiro argumento literal `build`) | `--get`/`--sync` são flags porque não têm positional próprio; `build` tem entry, saída e flags próprias | `noxy --build entry.nx`: `-o`/`--include` virariam flags globais da CLI |
| Reprodutível: entradas do zip em ordem lexicográfica, timestamps zerados, deflate | Mesmo projeto → mesmo payload → mesmo hash → mesmo diretório de cache | Timestamps reais: hash muda a cada build |
| macOS experimental na v1: o build no macOS **avisa**, sem código de `codesign` | Sem Mac para verificar (§11) | Re-assinar ad hoc no build: código não verificável agora |

## 3. Uso

### 3.1 `noxy build`

```
noxy build <entry.nx> [-o <saída>] [--include <caminho>]... [--list]
```

- `<entry.nx>`: o programa. A **raiz do projeto** é `FindRoot(dir(entry))` (o `noxy.mod` mais próximo); sem `noxy.mod`, a raiz é o diretório do entry. O entry precisa estar sob a raiz.
- `-o <saída>`: caminho do executável. Padrão: nome do entry sem `.nx`, no cwd. No Windows o build acrescenta `.exe` se faltar. A saída é escrita em `<saída>.tmp` e renomeada; sobrescreve como `go build`.
- `--include <caminho>`: arquivo ou diretório a embutir, relativo à raiz do projeto (barras `/`), repetível. Diretório entra recursivamente (arquivos regulares; links de diretório não são seguidos). Soma-se às linhas `include` do `noxy.mod`.
- `--list`: imprime em stdout o que entraria no payload e **não gera** o binário: entry, raiz, alvo, runtime, módulos (caminho relativo), extensões (nome, kind, plataforma, asset, tamanho) e arquivos incluídos (caminho, tamanho), com totais. Executa todo o plano (inclusive a checagem de compilação e a busca dos binários), então um asset ou binário faltando aparece aqui, sem rodar o executável.

Saída normal (stdout), uma linha por etapa e o resumo:

```
noxy build: 21 modules, 2 extensions (webview, pty), 7 included files
noxy build: wrote dist/noxy-editor (26.1 MB)
```

Diagnósticos vão em stderr (`diagOut`), exit code 1 em erro, 2 em uso inválido.

### 3.2 `include` no `noxy.mod`

```text
module noxy_editor

noxy v0.26.0

require github.com/estevaofon/noxy_pty v0.2.0
require github.com/estevaofon/noxy_webview v0.1.0

include web
```

`include <caminho>`, uma por linha, relativo ao diretório do `noxy.mod`, barras `/`, sem `..` nem caminho absoluto. `ParseModFile` guarda em `ModuleConfig.Include` (ordem do arquivo, sem duplicatas); `Save` reescreve as linhas depois dos `require`, em ordem lexicográfica — `--get` deixa de apagar a diretiva. `--sync`/`--get` não a usam. Um `noxy` anterior ignora a linha (parser já ignora diretivas desconhecidas).

### 3.3 O executável gerado (modo aplicação)

`./noxy-editor <args...>` (ou `noxy-editor.exe <args...>`):

1. Lê o trailer do próprio arquivo (`os.Executable` + `EvalSymlinks`). Sem trailer: é o `noxy` comum, nada muda.
2. Com trailer e **sem** `NOXY_INTERPRETER`: modo aplicação. Nenhuma flag da CLI é interpretada, não há REPL; todos os args vão ao programa.
3. Garante `<appdir>` extraído (§5.2) e roda `<appdir>/<entry>` com `RootPath = dir(<appdir>/<entry>)`, `argv = [os.Args[0], "<appdir>/<entry>", args...]`, cwd inalterado, mesmos tratador de sinais, `defer`s e exit codes de `noxy arquivo.nx`.
4. Com `NOXY_INTERPRETER` definida (qualquer valor não vazio): o payload é ignorado e o binário é o `noxy` (REPL, `--sync`, `noxy arquivo.nx`, e até `noxy build`).

Variáveis:

| Variável | Efeito |
|---|---|
| `NOXY_INTERPRETER=1` | Ignora o payload. **Herdada** pelos filhos: um programa rodado pelo F5 do editor que chame `sys.executable()` recebe o caminho do `noxy-editor`, e ao executá-lo obtém o interpretador, não o app |
| `NOXY_APP_CACHE=<dir>` | Troca `<UserCacheDir>/noxy/apps` por `<dir>` (testes, home somente-leitura). Sem `UserCacheDir` e sem a variável: erro `cannot determine the user cache directory; set NOXY_APP_CACHE` |
| `NOXY_PATH` | **Ignorada** em modo aplicação (resolução selada, §7.2) |

### 3.4 `sys.executable()`

`internal/stdlib/sys.nx`: `func executable() -> string` → nativo `sys_executable` (`os.Executable()` cru, sem `EvalSymlinks`; `""` em falha). Sob `noxy arquivo.nx` é o caminho do `noxy`; em modo aplicação, o do app. O padrão para "rodar um arquivo Noxy com o mesmo interpretador que me roda" é `NOXY_INTERPRETER=1 <sys.executable()> arquivo.nx`, com a variável **no ambiente do filho** (§8).

## 4. Formato

### 4.1 Layout do arquivo

```
[ runtime: bytes do noxy ][ payload: zip ][ trailer: 64 bytes ]
```

O runtime são os bytes do binário que roda o `noxy build`, de `0` até o offset do payload dele, se ele próprio tiver um (`NOXY_INTERPRETER=1 ./noxy-editor build x.nx` produz um app limpo, sem payload aninhado). A cópia passa por `filepath.EvalSymlinks(os.Executable())`; `go run ./cmd/noxy build ...` copia o binário temporário do `go run`, que é um noxy completo.

### 4.2 Trailer (64 bytes, little-endian)

| offset | tamanho | campo |
|---|---|---|
| 0 | 32 | sha256 do payload (dos bytes do zip) |
| 32 | 8 | offset do payload, contado do início do arquivo (u64) |
| 40 | 8 | tamanho do payload em bytes (u64) |
| 48 | 8 | reservado, zero |
| 56 | 8 | magic `NOXYAPP1` (ASCII; o `1` é a versão do trailer) |

Leitura (`bundle.Open(path)`): tamanho `< 64` ou magic ausente → **sem payload** (`ok=false`, sem erro). Magic presente e `offset + tamanho + 64 != tamanho do arquivo` (ou offset `>=` tamanho) → erro `app payload trailer is inconsistent with the file size`. O hash **não** é conferido no `Open`; só na extração (§5.2). `Open` devolve `Payload{Path, Offset, Size, SHA256}` e um `io.SectionReader` sobre o zip.

### 4.3 Payload (zip)

Entradas com caminho relativo à raiz do projeto, barras `/`, ordem lexicográfica, método deflate, timestamps zerados, modo `0755` para arquivos sob `bin/` ou com bit de execução no disco (Unix), `0644` para os demais.

| Entrada | Regra |
|---|---|
| `noxy-app.json` | manifesto (§4.4); nome reservado na raiz do projeto |
| `<entry>` | o programa, no caminho relativo à raiz (`editor.nx`, `noxy_examples/build_app.nx`) |
| módulos | todo módulo de arquivo alcançável por `use` (transitivamente), no caminho em que foi resolvido; módulo de diretório entra com todos os `.nx` e subdiretórios que a VM listaria como submódulos (`loadResolvedDirectory`) |
| extensões | para cada módulo com `noxy_ext.toml` ao lado: o manifesto e, `kind = "process"`, **só** `bin/<asset>` do alvo; `kind = "wasm"`, o `.wasm` do manifesto |
| `noxy.mod`, `noxy.sum` | quando existem na raiz (é o que faz `verifyExtensionSum` funcionar no `<appdir>`) |
| includes | os arquivos de `--include` e `include` (união, sem duplicata) |

Não entram: módulos da stdlib embutida (`stdlib.FS`, já no runtime); `bin/` de outras plataformas; `noxy_libs/.noxy-sync`; nada fora da raiz.

### 4.4 Manifesto `noxy-app.json`

```json
{
  "format": 1,
  "kind": "source",
  "noxy": "v0.26.0",
  "target": "linux/amd64",
  "entry": "editor.nx",
  "modules": [
    {"name": "src.session", "path": "src/session.nx"},
    {"name": "src", "path": "src", "dir": true}
  ],
  "extensions": [
    {"name": "webview", "kind": "process", "module": "github_com.estevaofon.noxy_webview.noxy_webview",
     "dir": "noxy_libs/github_com/estevaofon/noxy_webview",
     "artifact": "bin/noxy-plugin-webview-linux-amd64", "sha256": "…"}
  ],
  "includes": ["web"]
}
```

`format` é a versão do manifesto (um runtime que não conheça o número recusa com `unsupported app payload format N`); `kind` reserva `"bytecode"` para a v2; `noxy` é a `version.Version` do runtime copiado; `target` é `GOOS/GOARCH` do host (v1). `--list` é o manifesto em forma legível mais tamanhos.

## 5. Runtime (modo aplicação)

### 5.1 Detecção

`main()` chama `bundle.Open` no próprio executável **antes** de `flag.Parse`. Erro de leitura do próprio arquivo é tratado como "sem payload" (a CLI normal segue); trailer inconsistente é erro fatal em stderr, exit 1.

### 5.2 Extração atômica

`<base>` = `NOXY_APP_CACHE` ou `os.UserCacheDir()/noxy/apps` (Linux `~/.cache/noxy/apps`, Windows `%LocalAppData%\noxy\apps`, macOS `~/Library/Caches/noxy/apps`). `<appdir>` = `<base>/<16 primeiros hex do sha256 do payload>`.

1. Se `<appdir>/.noxy-app-ok` existe: pronto. **Nenhum hash é conferido** neste caminho; o marcador é a prova de extração íntegra.
2. Senão: lê o payload inteiro, confere sha256 contra o trailer (`app payload is corrupted: sha256 mismatch`), extrai o zip para `<base>/<hash>.tmp-<pid>-<aleatório>`, aplicando os modos do zip (`chmod` fora do Windows), recusando entradas com `..`, `\`, caminho absoluto ou letra de unidade (`app payload has an invalid path: <p>`), grava o marcador dentro do temporário e faz `os.Rename(tmp, <appdir>)`.
3. Se o `Rename` falha e `<appdir>/.noxy-app-ok` já existe (outra instância venceu a corrida, no Windows o rename sobre diretório existente falha): remove o temporário e segue. `<appdir>` sem marcador é sempre velho (só nasce do rename de um temporário que já tem o marcador — resto de extração interrompida ou diretório plantado): `RemoveAll(<appdir>)` e o rename é tentado uma vez mais. Falha que persiste sem marcador: `cannot extract app payload to <appdir>: <err>`, sem deixar o temporário.

Um build novo tem outro hash e outro diretório; os antigos ficam (limpeza fora de escopo, documentada em `docs/BUILD.md`: apagar `<base>` é seguro).

### 5.3 Execução

`RootPath = dir(<appdir>/<entry>)`; `ProjectRoot = <appdir>` quando `<appdir>/noxy.mod` existe (o `noxy.mod` extraído, quando o projeto tinha um), senão `""` — **nunca** `FindRoot`, que subiria acima de `<appdir>` e deixaria um `noxy.mod` alheio (e o `noxy_libs` dele, que precede `<Root>/noxy_libs`) sombrear o app; a VM com `Source` própria também não completa `ProjectRoot` sozinha; `VMConfig.Source` = `DiskSource` selada (§7.2); `os.Args` remontado antes de criar a VM; `runFile` como hoje (sinais, `CloseExtensions`, `CloseProcesses`, exit codes). Extensões carregam de `<appdir>/noxy_libs/.../bin/<asset>` pelo caminho de sempre e `verifyExtensionSum` confere contra `<appdir>/noxy.sum` como num projeto sincronizado.

## 6. Build (`internal/build`)

`build.Plan(opts) (*Plan, error)` monta o plano sem escrever nada (é o que `--list` imprime); `build.Write(plan, opts) error` gera o arquivo.

1. **Raiz.** `entry` absoluto; `root = FindRoot(dir(entry))` ou `dir(entry)`; `entry` fora de `root` → `entry <p> is outside the project root <root>`.
2. **Grafo de `use`.** Parse do entry; para cada `ast.UseStmt`, `Source.Resolve(nome)` com a `DiskSource` **não selada** (a mesma resolução de `noxy entry.nx`, `NOXY_PATH` inclusive). Kind arquivo: registra `{nome, caminho relativo}` e desce nos `use` dele; diretório: registra o diretório, lista com `ReadDir` e desce em cada `.nx` e subdiretório como `nome.sub`; embutido (stdlib): ignora. Memoizado por nome; ciclos não são erro aqui (a VM os detecta em runtime). Subdiretório de módulo de diretório que **não resolve** é pulado, como a VM faz; o que resolve e **não compila** é erro no build (a VM o omitiria em silêncio — `TestRuntimeDirectoryModuleGlobalsContainOnlyLoadableChildren` —, mas "erro sai no build" vale mais que espelhar essa tolerância). Caminho resolvido fora de `root` (lexicalmente, sem `EvalSymlinks`, para um package linkado por symlink dentro de `noxy_libs` continuar valendo) → `module <nome> resolves to <p>, outside the project root <root>`. Não encontrado → `<arquivo>:<linha>: module not found: <nome>` + `pkgmanager.SyncHint`. **Checagem selada:** cada módulo resolvido é resolvido de novo com `modsrc.NewSealed(dir(entry), <raiz do noxy.mod ou "">)` — a origem que o app usará, com o mesmo layout relativo — e tem de dar o mesmo `Kind` e o mesmo caminho; senão `module <nome> resolves through the current directory or NOXY_PATH (<p>); a built executable only searches the project — move it under noxy_libs/ or next to the entry` (um módulo achado só pelo cwd ou pelo `NOXY_PATH`, mesmo dentro da raiz, passaria no build e faltaria no app).
3. **Checagem de compilação.** Uma VM (`vm.NewWithConfig{RootPath: dir(entry)}`) e, em pré-ordem (a ordem em que a VM carregaria), para cada módulo de arquivo: se há `noxy_ext.toml` ao lado, `ensureExtensionLoaded(dir)` (não sobe processo; registra os nativos); depois `vm.CompileModule(m)`, a metade "compilar" de `compileAndRunModule`, extraída para isso. O entry é compilado como em `runWithConfig`. Avisos vão para stderr; o primeiro erro aborta com `<caminho relativo>: <erro do compilador>`. `sys_load_plugin` no AST de qualquer arquivo → `sys_load_plugin is not supported in built executables (removed in v0.27.0)`.
4. **Extensões.** Para cada manifesto: `kind = "process"` → `BinaryFor(GOOS, GOARCH)` do host; ausente no manifesto → `extension "<nome>" has no binary for <goos>/<goarch> (published: …)`; ausente em `bin/` → `extension "<nome>": binary bin/<asset> not found — run 'noxy --sync' to download it`. O hash contra `noxy.sum` já foi conferido pelo `ensureExtensionLoaded` do passo 3; o sha256 do asset vai ao manifesto. `kind = "wasm"` → o `.wasm`.
5. **Includes.** União de `--include` e `ModuleConfig.Include`, validados (relativo, sem `..`, sob `root`): `include <p> not found` / `include "<p>" is outside the project root`. Diretórios por `filepath.WalkDir` sobre `EvalSymlinks(<include>)` (um include que é symlink é seguido), com o caminho no payload lexical (`<include>/<rel>`); dentro dele, symlink para arquivo entra (lido pelo link), symlink para diretório não é seguido; include sem nenhum arquivo → `include <p> contains no files`. Arquivo já presente como módulo não duplica.
6. **Escrita.** Zip determinístico em memória (§4.3), sha256, `<saída>.tmp` = runtime (§4.1) + zip + trailer, `chmod 0755`, `Rename`. `runtime.GOOS == "darwin"` → `warning: macOS output is experimental and was not validated on this platform (see docs/BUILD.md)` em stderr.

## 7. `internal/modsrc`: uma origem de módulos

### 7.1 Interface

```go
type Kind uint8            // KindFile, KindDirectory, KindEmbedded
type Module struct {
    Name    string         // "src.session"
    Kind    Kind
    Path    string         // absoluto e limpo (File/Directory); "" para Embedded
    Content string         // só Embedded (stdlib.FS)
}
type Entry struct { Name string; IsDir bool }

type Source interface {
    Key() string                              // identidade para o cache de módulos (raiz real + search paths + selo)
    Resolve(name string) (Module, error)      // ErrNotFound embrulhado quando não há candidato nem embed
    ReadFile(path string) ([]byte, error)
    ReadDir(path string) ([]Entry, error)
}
```

`ErrNotFound` é sentinela; quem chama acrescenta o `SyncHint` (VM em `loadModule`, compilador no `use`).

### 7.2 `DiskSource`

```go
type DiskSource struct {
    Root        string   // RootPath (diretório do script)
    ProjectRoot string   // FindRoot(Root) ou ""
    SearchPaths []string // NOXY_PATH já dividido; nil quando selada
    SearchCwd   bool     // candidatos relativos ao cwd; false quando selada
}
func NewDisk(root, projectRoot string) *DiskSource   // lê NOXY_PATH, SearchCwd=true
func NewSealed(root, projectRoot string) *DiskSource // modo aplicação
```

`Resolve` reproduz a ordem de hoje (§0, linha 1), arquivo antes de diretório, entrada `<base>.nx`/`main.nx` dentro de diretório, e cai em `stdlib.FS` por último. `Key()` = `EvalSymlinks(Root)` + `\x00` + search paths + `\x00` + selo, no lugar do `root + "\x00" + NOXY_PATH` de `moduleKey.Root`.

### 7.3 Quem passa a usar

- VM: `VMConfig.Source` (nil → `NewDisk(RootPath, ProjectRoot)` em `NewWithConfig`); `resolveModule` vira `vm.Config.Source.Resolve`; `loadResolvedModule`/`loadResolvedDirectory` leem por `Source`; `ensureExtensionLoaded` lê o manifesto e o `.wasm` por `Source.ReadFile` e continua entregando ao processo o caminho real de `bin/<asset>`. `loadModule` fica em `modules.go` (guarda de arquitetura). Os compiladores de módulo (`compileAndRunModule`) recebem a mesma `Source`.
- Compilador: `c.moduleSource` (`SetModuleSource`), propagado em `NewChild`, `newPass1Compiler` e no `validator` de `parseModuleDeclarations`; `moduleFileCandidates` e os `os.*` de `resolveModuleDeclarations`/`parseModuleDeclarationsFile` saem. Sem `Source` explícita, `NewWithStateAndRoot` cria `NewDisk(moduleRoot, projectRoot)`, então quem embute o compilador sem VM não muda.
- Build: `NewDisk` (não selada) para o grafo; a VM da checagem usa a mesma.

## 8. Fecho no Noxy-Editor (repositório `noxy_projects/Noxy-Editor`)

- `noxy.mod`: `noxy v0.26.0` e `include web`.
- `src/runner.nx`: o comando do F5 troca `noxy` por `sys.executable()` e põe `NOXY_INTERPRETER=1` **no ambiente do filho**, nunca no do editor: Unix `cd <raiz> && NOXY_INTERPRETER=1 exec '<exe>' '<rel>'`; Windows, na linha do `cmd`, `set NOXY_INTERPRETER=1&& "<exe>" "<rel>"` pelo mesmo caminho (`platform.arg`/`Start-Process`) que já leva o comando. Sob `noxy editor.nx` o resultado é idêntico ao de hoje (`sys.executable()` é o `noxy`).
- README do editor: seção "Distribuir" com `noxy build editor.nx -o dist/noxy-editor`, a nota de que o terminal do editor não ganha um `noxy` no PATH (o app não instala nada) e que macOS é experimental.
- Este fecho é a verificação do critério de aceite (§1) e fica no plano como última tarefa, manual: janela, terminal, F5.

## 9. Testes

| Pacote | Testes |
|---|---|
| `internal/bundle` | trailer ida e volta; arquivo sem magic → `ok=false` sem erro; magic com offset/tamanho inconsistentes → erro literal; manifesto ida e volta e `format` desconhecido; zip determinístico (dois builds do mesmo diretório → mesmo sha256); extração confere hash (byte trocado → erro, nada fica em `<base>`); extração idempotente (segunda chamada com payload corrompido mas marcador presente → sem erro: prova que o hash é só na extração); entrada com `..`/absoluta → erro; bit de execução em `bin/` (Unix); `RuntimeBytes` de um app devolve só `[0:offset)`; corrida: dois extratores concorrentes terminam com um `<appdir>` válido |
| `internal/modsrc` | ordem de candidatos (`NOXY_PATH` > projeto > raiz; arquivo antes de diretório; `<base>.nx`/`main.nx`; embed por último); selada ignora `NOXY_PATH` e cwd; `Key` muda com selo e search paths; `ReadDir` lista `.nx` e subdiretórios |
| `internal/compiler`, `internal/vm` | os testes de módulos existentes (`module_*_test.go`, `generics_modules_e2e_test.go`, `process_extensions_e2e_test.go`, `extensions_e2e_test.go`) passam inalterados sobre a `Source`; um teste novo em cada pacote injeta uma `Source` de memória (mapa nome → conteúdo, no próprio arquivo de teste) e prova que nem compilador nem VM tocam o disco para resolver um módulo |
| `internal/build` | grafo transitivo (entry → `src.a` → `src.b`, módulo de diretório com submódulos); stdlib embutida não entra; `.nx` local que sombreia a stdlib entra; erro de compilação num módulo sai com o caminho do módulo; módulo fora da raiz; include de `noxy.mod` + flag unidos e sem duplicata; include ausente; extensão por processo com o guest de `exttest` (asset e sha256 no plano); binário ausente → texto literal com `--sync`; `sys_load_plugin` recusado; `--list` imprime módulos, extensão com plataforma e includes |
| `internal/pkgmanager` | `include` parseado, preservado por `Save`, rejeitado com `..`/absoluto; `noxy.mod` sem `include` continua idêntico após `Save` |
| `cmd/noxy` (integração, `build_test.go`) | compila o `noxy` com `go build` (como `sync_flags_test.go`). (a) `noxy build noxy_examples/build_app.nx --include noxy_examples/build_app_assets -o <tmp>/app` da raiz do repositório: `build_app.nx` usa `math_lib` (package em `noxy_libs`) e lê `dirname(argv[1]) + "/build_app_assets/greeting.txt"`; roda `<tmp>/app um dois` de **outro** cwd, sem `noxy_libs` nele, com `NOXY_APP_CACHE` num temporário e `PATH` sem `noxy`; confere stdout (saudação do asset, resultado do `math_lib`, `um dois`) e que `<base>` tem exatamente um `<appdir>` com marcador; segunda execução não re-extrai (mtime do marcador). (b) projeto temporário com extensão por processo (`writeProcessExtensionPackage`-like com `exttest.BuildProcessGuest`), `noxy build` + execução de outro cwd, stdout com o resultado do `guest_add`. (c) `NOXY_INTERPRETER=1 <app> outro.nx` roda o interpretador; `NOXY_INTERPRETER=1 <app> build …` produz um app sem payload aninhado. (d) `noxy build --list` sem gerar arquivo; erro de compilação num módulo sai no build com exit 1 e nome do arquivo; `noxy build` de um módulo fora da raiz falha. O exemplo `noxy_examples/build_app.nx` também roda no runner de exemplos como programa comum |

CI: nada a adicionar — `go test ./internal/... -count=1` e `go test ./cmd/... -count=1` já rodam em Ubuntu e Windows (`.github/workflows/network-deadlines.yml`).

## 10. Documentação

- `docs/BUILD.md` (novo): uso; includes (flag e `noxy.mod`); como o app roda (cache, `argv`, cwd, `NOXY_INTERPRETER` herdada, `NOXY_APP_CACHE`, `NOXY_PATH` ignorada); o padrão do F5 (`sys.executable()` + variável no filho); o que há no payload e que **o fonte é legível por qualquer um com `unzip`**, e que bytecode na v2 também não protegerá o código; plataformas: Linux e Windows; macOS experimental com o plano B (segmento Mach-O como Node SEA/`postject` e o `sui` do Deno) e a nota sobre Gatekeeper/notarização; Windows: AV pode inspecionar o `.exe` e os plugins extraídos, mesmo caso do `noxy.exe`; tamanho e `unzip -l app` para inspecionar; limitações (§12).
- `README.md`: seção "Standalone executables" após "Usage", quatro linhas e link para `docs/BUILD.md`.
- `CHANGELOG.md`: em `[0.26.0]`, `Added` (`noxy build`, modo aplicação, `sys.executable`, `include` no `noxy.mod`) e `Changed` (resolução de módulos unificada em `internal/modsrc`; `noxy build` passa a ser subcomando, então um arquivo chamado literalmente `build` sem extensão precisa de `./build`).
- `docs/NOXY_LANGUAGE_SPEC.md` §12 (`sys`): `executable()`.
- `AGENTS.md`: `internal/modsrc`, `internal/bundle`, `internal/build` na tabela; `noxy build` e o modo aplicação em `cmd/noxy`; regra "resolução de módulo só por `modsrc.Source`".
- `docs/*.md` passa pelo Liquid: nenhum `{{` fora de `raw`.

## 11. Riscos e plataformas

| Risco | Avaliação | Mitigação na v1 |
|---|---|---|
| macOS: o Go assina ad hoc os binários darwin; bytes depois da assinatura ficam fora do trecho assinado (`codeLimit`), então a execução local tende a passar, mas `codesign --verify --strict`, Gatekeeper e notarização rejeitam; em arm64 uma assinatura considerada inválida mata o processo | Não verificável sem Mac | Alvo da v1 é Linux e Windows; o build no macOS avisa; `docs/BUILD.md` marca experimental e cita o plano B (segmento Mach-O) |
| Windows: antivírus/SmartScreen com `.exe` não assinado e blob anexado | Igual ao `noxy.exe` de hoje; blob anexado é padrão de instalador | Documentar; nada no código |
| `os.Executable` com symlink | Linux devolve o alvo real; macOS/Windows podem devolver o link; abrir segue o link | `EvalSymlinks` antes de copiar; leitura do trailer funciona pelos dois caminhos |
| Tamanho | 23,8 MB de runtime sem strip + payload comprimido (Go comprime ~50%, texto ~70%): editor ≈ 26 MB no Linux | Documentar; strip do runtime é decisão da release do noxy |
| Custo no `noxy` comum | um `open` + `read` de 64 bytes por start | — |
| Cache compartilhado por hash | mesmo payload → mesmo diretório; usuário pode apagar `<base>` a qualquer hora | marcador + extração atômica; sem hash por start (decisão) |

## 12. Fora de escopo e continuações

1. **Bytecode (`kind: "bytecode"`) e `PayloadSource`.** Serializar Chunk e constantes; compilar tudo no build (instâncias genéricas já resolvidas); trocar `compileAndRunModule` por execução do chunk; ler módulos de dentro do zip. Não protege o código: bytecode é desmontável (`--disassembly`).
2. **Cross-compile (`--target goos/goarch`).** Entra quando a release do noxy publicar `noxy-<goos>-<goarch>[.exe]` + `checksums.txt` (workflow como `sdk/noxyplugin/release/github-release.yml`): o build baixa o runtime do alvo e o asset do plugin do alvo (URL por `ReleaseBaseURL`, hash já no `noxy.sum`). Sem `--runtime`.
3. **macOS**: validar; se preciso, segmento Mach-O e re-assinatura ad hoc.
4. **Módulo `assets`** read-only (`assets.read`, `assets.list`) para programas que não querem caminho no disco.
5. **Limpeza de caches** antigos (`noxy build --clean-cache` ou por idade).
6. **`sys_load_plugin`**: não suportado; some na v0.27.0.
