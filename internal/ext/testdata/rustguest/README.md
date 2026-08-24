# rustguest — ABI v1 reference guest

Minimal Noxy extension guest in Rust: no WASI, only `noxy:host/v1` imports.
Used by `internal/ext/rustguest_test.go` as the capability-free load fixture
and for the spec §11 benchmarks. **The compiled `rustguest.wasm` is committed**
because CI has no Rust toolchain; rebuild it after editing `src/lib.rs`:

    rustup target add wasm32-unknown-unknown
    cargo build --release --target wasm32-unknown-unknown
    cp target/wasm32-unknown-unknown/release/rustguest.wasm rustguest.wasm
