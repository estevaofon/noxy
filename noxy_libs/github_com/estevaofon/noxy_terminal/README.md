# Noxy Terminal

`noxy_terminal` is an experimental native terminal-input module. It currently
lives in this repository at `noxy_libs/github_com/estevaofon/noxy_terminal`.

## Prerequisites and build

Building the plugin requires Go 1.24 or newer. From this directory, run one of
the following commands:

```powershell
.\build_plugin.ps1
```

```bash
./build_plugin.sh
```

The build creates `noxy-plugin-terminal` (`noxy-plugin-terminal.exe` on
Windows). Keep that binary next to the program that loads this module, or make
it available on `PATH`.

## Use

Import `github_com.estevaofon.noxy_terminal.terminal`:

```noxy
use github_com.estevaofon.noxy_terminal.terminal

let opened: terminal.TerminalResult = terminal.open_raw()
if opened.ok then
    let event: terminal.KeyEvent = terminal.read_key()
    terminal.close()
end
```

Public API:

```noxy
struct TerminalResult
    ok: bool
    error: string
end

struct KeyEvent
    ok: bool
    key: string
    error: string
end

func is_terminal() -> bool
func open_raw() -> TerminalResult
func read_key() -> KeyEvent
func close() -> bool
```

`read_key()` returns lowercase letters for uppercase alphabetic input and the
named keys `space`, `enter`, and `ctrl+c`. Other printable characters are
returned as themselves; unsupported control input is represented as
`unknown:0xNN`.

Only one reader may call `read_key()` at a time. Call `close()` explicitly
after opening raw mode, including on every normal game exit, so the terminal
settings are restored promptly. Calling `close()` before opening is safe.

On Windows the plugin opens `CONIN$`; on Unix-like systems it opens `/dev/tty`.
This lets interactive terminal input work even when the program's standard
input is redirected, provided that device is available.

## Space Invaders

Build the Noxy executable and the terminal plugin, then run the example from
the repository root:

```powershell
go build -o noxy.exe ./cmd/noxy
.\noxy_libs\github_com\estevaofon\noxy_terminal\build_plugin.ps1
$env:PATH = "$(Resolve-Path .\noxy_libs\github_com\estevaofon\noxy_terminal);$env:PATH"
.\noxy.exe noxy_examples\space_invaders.nx
```

```bash
go build -o noxy ./cmd/noxy
./noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.sh
PATH="$(pwd)/noxy_libs/github_com/estevaofon/noxy_terminal:$PATH" ./noxy noxy_examples/space_invaders.nx
```
