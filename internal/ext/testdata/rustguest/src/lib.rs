//! Guest de referencia do ABI v1 do Noxy (spec §2): sem WASI, sem imports
//! fora de `noxy:host/v1`. Fixture do gate positivo de imports e dos
//! benchmarks da §11. fn_index: 0 echobytes, 1 fail, 2 trap, 3 sha256,
//! 4 empty (regiao de resultado vazio, ver ret_raw/nx_free).

use sha2::{Digest, Sha256};
use std::alloc::{alloc, dealloc, Layout};

#[link(wasm_import_module = "noxy:host/v1")]
extern "C" {
    fn nx_fail(ptr: u32, len: u32);
    #[allow(dead_code)]
    fn nx_log(level: u32, ptr: u32, len: u32);
}

#[no_mangle]
pub extern "C" fn nx_abi_version() -> u32 {
    1
}

#[no_mangle]
pub extern "C" fn nx_alloc(size: u32) -> u32 {
    if size == 0 {
        return 0;
    }
    let layout = Layout::from_size_align(size as usize, 1).unwrap();
    unsafe { alloc(layout) as u32 }
}

/// Regiao de resultado vazio tem 1 byte real (espelha ret_raw abaixo) — 0 e
/// o sentinela de falha do ABI, entao ret_raw aloca 1 byte mesmo quando o
/// payload esta vazio. nx_free precisa desalocar esse mesmo tamanho real,
/// senao a regiao vaza a cada resultado vazio numa instancia reusada.
#[no_mangle]
pub extern "C" fn nx_free(ptr: u32, size: u32) {
    if ptr == 0 {
        return;
    }
    let real_size = if size == 0 { 1 } else { size as usize };
    let layout = Layout::from_size_align(real_size, 1).unwrap();
    unsafe { dealloc(ptr as *mut u8, layout) }
}

/// Devolve `data` numa regiao nova: (ptr << 32) | len. Payload vazio ainda
/// aloca 1 byte — 0 e o sentinela de falha do ABI.
fn ret_raw(data: &[u8]) -> u64 {
    if data.is_empty() {
        let ptr = nx_alloc(1);
        return (ptr as u64) << 32;
    }
    let ptr = nx_alloc(data.len() as u32);
    let out = unsafe { core::slice::from_raw_parts_mut(ptr as *mut u8, data.len()) };
    out.copy_from_slice(data);
    ((ptr as u64) << 32) | (data.len() as u64)
}

/// NXB bytes: tag 0x05 + u32 LE len + payload.
fn ret_nxb_bytes(payload: &[u8]) -> u64 {
    let mut out = Vec::with_capacity(5 + payload.len());
    out.push(0x05);
    out.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    out.extend_from_slice(payload);
    ret_raw(&out)
}

fn fail(msg: &str) -> u64 {
    unsafe { nx_fail(msg.as_ptr() as u32, msg.len() as u32) }
    0
}

#[no_mangle]
pub extern "C" fn nx_call(fn_index: u32, args_ptr: u32, args_len: u32) -> u64 {
    let args: &[u8] = if args_len == 0 {
        &[]
    } else {
        unsafe { core::slice::from_raw_parts(args_ptr as *const u8, args_len as usize) }
    };
    match fn_index {
        // echobytes: args = u32 count + um valor NXB bytes; devolve o valor
        // tal qual (ja e tag+len+payload) — cópia pura, sem compute.
        0 => {
            if args.len() < 4 {
                return fail("echobytes expects one bytes argument");
            }
            ret_raw(&args[4..])
        }
        1 => fail("boom from rust guest"),
        2 => core::arch::wasm32::unreachable(),
        3 => ret_nxb_bytes(&Sha256::digest(args)),
        // empty: exercita o ramo de payload vazio de ret_raw (ptr real,
        // len 0) — o host ve uma regiao de 0 bytes, o que nao decodifica
        // como NXB valido; fixture do fix de nx_free acima.
        4 => ret_raw(&[]),
        _ => fail("unknown fn_index"),
    }
}
