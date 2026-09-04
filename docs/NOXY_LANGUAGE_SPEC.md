# Noxy Language Specification

<!-- {% raw %} -->
<!-- The markers above and below are HTML comments (invisible on GitHub): they keep
     GitHub Pages' Liquid renderer from interpreting the literal {{ and }} in this
     document (f-string escapes). Do not remove them. -->

## Overview

Noxy is statically typed, with explicit dynamic boundaries through `any`, bare
`func`, untyped native primitives, and plugins without signatures. Designed for
educational purposes and practical applications, it supports structs,
references, arrays, f-strings, and a module system.
The current implementation is a **Stack-based VM** written in **Go**.

### The Zen of Noxy

The philosophy that guides the language and its future decisions — short by
design, in the spirit of the Zen of Python: a compass, not a rulebook.

```text
Simplicity is sophistication.
Typing is safety — and the compiler speaks first.
Dynamic exists, but it is explicit: any says what it is.
Variables are copies, unless explicitly stated otherwise.
Sharing is ref — in the type and at the call site. Closures and globals share by name; nothing else does.
CoW + ref is one heck of a duo!
An error is a value, not an exception.
One rule, everywhere: file, module, REPL.
Consistency comes before performance.
Performance is measured afterwards — without changing semantics.
Lean core, vast ecosystem.
Fixing beats staying compatible, until 1.0 says otherwise.
```

---

## 1. Lexical Structure

### 1.1 Comments

```noxy
// Single line comment
```

### 1.2 Keywords

| Category | Keywords |
|----------|----------|
| Declarations | `let`, `func`, `struct` |
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break`, `continue`, `for`, `in`, `defer`, `when`, `case`, `default`, `try` |
| Types | `int`, `float`, `string`, `bool`, `void`, `bytes`, `any`, `ref`, `func`, `map`, `chan` |
| Literals | `true`, `false`, `null` |
| Modules | `use`, `select`, `as` |
| Specials | `zeros` |

`map` and `chan` are type constructors (`map[K, V]`, `chan T`), `any` is the
dynamic type (§2.1); all three are reserved everywhere, so they cannot name a
variable, a parameter or a module (`use src.map` → `'map' is a keyword and
cannot be used as a name`). `str` is not a keyword.

### 1.3 Operators

| Category | Operators |
|----------|-----------|
| Arithmetic | `+`, `-`, `*`, `/`, `%` |
| Comparison | `>`, `<`, `>=`, `<=`, `==`, `!=` |
| Logical | `&&`, `||`, `!` |
| Bitwise | `&`, `|`, `^`, `~`, `<<`, `>>` |
| Assignment | `=` |
| Reference | `ref` |
| Function Return | `->` |

### 1.4 Delimiters

| Symbol | Usage |
|--------|-------|
| `(` `)` | Parentheses for expressions and function calls |
| `[` `]` | Brackets for arrays and indexing |
| `{` `}` | Braces for f-string interpolation and map literals |
| `,` | Separator for parameters and elements |
| `:` | Separator for types in declarations |
| `.` | Access struct fields or module members |
| `?` | Type suffix: `T?` is `T` or `null` (§2.4) |

---

## 2. Type System

### 2.0 Fundamental Typing Rules

#### Static Typing and Type-Stable Variables

Noxy uses **type-stable variables**: a variable's declared type remains stable,
while the value stored in it may be mutable.

1. **The type of a variable is defined at declaration and cannot be changed.**
2. Attempts to assign a value of a different type result in a compilation error.
3. There is no implicit conversion between types (except where explicitly documented).
4. **`null` is not a value of any type but `T?`, `any` and `null` itself.** A
   bare struct, `ref T`, array or primitive never holds `null`; a slot that
   may be empty says so in its type, with the `?` suffix (§2.4).

```noxy
let x: int = 42
x = 100          // ✓ OK - same type (int)
x = 3.14         // ✗ ERROR - cannot assign float to int variable
x = "text"       // ✗ ERROR - cannot assign string to int variable
```

#### Compile-Time Type Checking

- Outside the explicit dynamic boundaries listed above, type errors are detected
  **before** execution.
- The compiler checks compatibility in assignments, exact function calls, and
  operations. Dynamic boundaries validate the contracts available at runtime.
- A type annotation must name a type that exists — a primitive, a struct
  declared or imported into the program, a qualified `m.T`, or a generic
  instance. `let x: Inexistente = 1`, `func f(x: Inexistente)`, a return type
  or a struct field naming an unknown type is a compile error (`unknown type
  'Inexistente'` + hint), never a runtime failure — see §11, *Unknown type
  names*.

### 2.1 Primitive Types

| Type | Description | Example |
|------|-------------|---------|
| `int` | 64-bit Integer | `42`, `-10`, `0` |
| `float` | Double precision Floating Point | `3.14`, `-0.5`, `1.0`, `1.5e3`, `2E-10` |
| `string` | Character string | `"Hello"`, `""` |
| `bool` | Boolean value | `true`, `false` |
| `void` | Absence of value (function return only) | - |
| `bytes` | Raw byte sequence | `b"Data"`, `hex_decode("FF")` |

An `int` literal must fit in the signed 64-bit range
`-9223372036854775808 … 9223372036854775807`. A literal outside that range —
decimal, hexadecimal (`0x…`) or binary (`0b…`) — is a compile-time error,
never a silently saturated value. Unary minus applied directly to an integer
literal is part of the literal, so the minimum of the type is writable as
`-9223372036854775808`:

```noxy
let min: int = -9223372036854775808 // ok — exact
let bad: int = 9223372036854775808  // ERROR: integer literal out of int64 range
```

A `float` literal accepts scientific notation: an exponent `e`/`E`, optionally
signed, after the digits (and after the optional fraction). The exponent makes
the literal a `float` even without a decimal point.

```noxy
let a: float = 1e3      // 1000.0
let b: float = 1.5e-3   // 0.0015
let c: float = 2E+10    // 20000000000.0
```

A leading dot is **not** accepted: write `0.5`, never `.5`. An `e` that is not
followed by (an optional sign and) a digit is not part of the literal.

### 2.2 Composite Types

#### Value Semantics (Copy-on-Write)

Arrays, maps, and structs are composite **values**. Every binding without
`ref` behaves as an independent deep copy, at any depth through every *value*
field — a field, element or map value declared `ref T` is the one edge a copy
shares (rule 6):

1. **Assignment copies**: `let b = a` and `x = y` produce independent values.
2. **Calls copy**: arguments to non-`ref` parameters are independent values —
   nested mutation inside the callee never leaks to the caller.
3. **Reading from a container copies**: `let p = arr[0]` produces an
   independent value; mutating `p` does not affect `arr[0]`.
4. **Storing into a container copies**: `append(ref outer, inner)`, `m[k] = v`,
   `s.field = arr`, and constructor arguments store independent values.
5. **Channels carry values**: `chan_send` delivers an independent copy. This
   applies to `spawn` and `spawn_task` arguments equally.
6. **`ref` is the only sharing mechanism visible in a type.** A `ref` points
   to a *slot* (variable, field, index, map entry); writes through any alias
   of the slot are visible to all aliases of that slot. Closures capture
   variables by name and globals are shared by name — those are the only
   other places where two names can see one slot, and the only ones that
   need coordination under concurrency (see §2.3 R9).
   A `ref` **field** (or a `(ref T)[]` element, or a `map[K, ref T]` value)
   is such a slot edge *inside* a value: copying the container copies the
   edge, not its target, so the original and every copy reach the same
   target. A type is **ref-carrying** when any field, element or map value
   is `ref T` or is itself ref-carrying. Rules 1–5 hold for a ref-carrying
   value exactly as for any other — the copy is independent through every
   value field — but the sharing is declared in the *type*, at the struct
   declaration, and does not appear at the assignment, call site, container
   read or channel that copies it. To own a nested composite instead of
   sharing it, declare the field without `ref` (`next: Node?`, nullable
   because the list ends) — see §5 *Self-Reference* for both idioms.
7. **`==`/`!=` on composites is structural** (recursive by content). `ref`
   values compare by slot identity and are not dereferenced — both as fields
   nested inside a composite and as the two operands of a direct comparison.
   A `ref` compared against `null` asks whether the reference *itself* is
   null, and a `ref` compared against a plain value is a compile-time error:
   the read must be explicit (`*r == value`); see §2.3 R7.
8. Closures capture *variables* (slots) by name; that is the second way two
   names can see one slot (rule 6).

The runtime implements this contract with **copy-on-write**: no copy is made
at the binding site — composites are marked as shared and cloned lazily, one
level at a time, at the first mutation. Read-only sharing therefore costs
O(1); programs never observe the difference, only the performance.

#### Concurrency and composite values

Shared routines use synchronized global bindings, module state, maps, and runtime handle registries. An individual binding lookup/update or map operation is safe from the Go runtime's concurrent-map crash, but synchronization is not recursive and does not make a read-modify-write sequence atomic. Struct fields and array elements have no per-operation synchronization: a read concurrent with a write is a data race and may observe a torn value, but it never brings the Go runtime down — an instance keeps its fields in declaration-order slots, not in a Go map (issue #86). Normal calls, `spawn`, and `spawn_task` all follow the value-semantics parameter rules above — the legacy `spawn` identity exception was removed in 0.4.0 — so data handed to another routine by argument or channel is race-free by construction — unless its type is ref-carrying (rule 6): a `ref` field travels as a shared edge and needs the same coordination as a `ref` argument. Concurrent mutation of intentionally shared state (globals, `ref` — as argument or as field) still requires coordination through channels or another explicit single-owner protocol.

#### Arrays (Dynamic and Fixed)

**1. Dynamic Arrays (Recommended)**
```noxy
// Declaration (starts empty)
let dynamic: int[] 

// Operations
append(ref dynamic, 10)
length(dynamic)
```

**2. Fixed Size Arrays**
```noxy
let fixed: int[5] = [1, 2, 3, 4, 5]
let zeroed: int[100] = zeros(100)
```

**Pass-by-Value Behavior**:
Arrays are passed by **VALUE**: the callee's array is independent at any depth through value elements (copy-on-write; a `(ref T)[]` element is a shared edge — §2.2 rule 6). Use `ref` when the function must modify the caller's array.

#### Maps (Hashmaps)

```noxy
// Type: map[Key, Value]
let scores: map[string, int] = {"Alice": 100}
scores["Bob"] = 50
```

**Pass-by-Value Behavior**:
Maps are passed by **VALUE**: the callee's map is independent at any depth through value entries (copy-on-write; a `map[K, ref T]` value is a shared edge — §2.2 rule 6). Use `ref` when the function must modify the caller's map.

**Writing with a dot**: on a value whose static type is `map[K, V]`,
`m.key = v` is checked exactly like `m["key"] = v` — the key type must accept
`string` (`m.a = 2` on a `map[int, int]` is `[line N] type mismatch in map
key: expected int, got string`) and the value must match `V` (`m.a = "boom"`
on a `map[string, int]` is `[line N] type mismatch in map value: expected
int, got string`). The two forms differ in what they do to a key that is not
there: the index form **inserts** it, while the dot form only addresses an
**existing** entry, in both directions — reading or writing a missing key is
the runtime error `undefined property 'b' in module/map`. A dot *read* on a
map stays dynamically typed: unlike the write, it is not checked against `V`.

**Iteration order is undefined**: a map is backed by a Go map, so `for k in m`,
`keys(m)` and printing a map may produce a different order on each run and
across versions. Never depend on it — sort `keys(m)` when the output has to be
stable.

#### Structs

```noxy
struct Person
    name: string
    age: int
end
```

**Pass-by-Value Behavior**:
Structs are passed by **VALUE**: the callee's instance is independent at any depth through value fields (copy-on-write), including nested composite fields declared without `ref`. A field declared `ref T` is a shared edge: the callee's copy reaches the caller's target (§2.2 rule 6). Use `ref` when the function must modify the caller's original instance.

---
### 2.3 References (`ref`)

A reference is never created or read without `ref` or `*` in the source.
Three forms, and one shortcut:

| Form | Meaning |
|---|---|
| `ref x` | create a reference to the slot of `x` |
| `r` | the reference itself (where it points) |
| `*r` | the referenced value — read it, or write it with `*r = v` |
| `r.f`, `r[i]` | shortcut for `(*r).f`, `(*r)[i]` — reads and writes |

#### R1. `ref x` creates a reference

`x` must be **addressable**: a local or global variable, a struct field, an
array element, a map entry, or a captured variable. A non-null literal or a
temporary (the result of a call whose type is not `ref T`) is not:

```noxy
let err: Error = Error("msg")
let r: ref Error = ref err          // OK
let bad: ref Error = ref Error("m") // ERROR: reference argument 'Error("m")' is not addressable
```

If `x` already has type `ref T`, `ref x` is an error — a reference is passed
as any other value, never re-referenced. There is no `ref ref T`; the
annotation is rejected by the parser.

```noxy
let r: ref int = ref x
f(ref r)      // ERROR: 'r' is already a reference
              //   hint: pass 'r' directly, without 'ref'
f(r)          // OK
```

#### R2. A `ref T` is never read implicitly

Wherever the compiler expects a `T` and finds a `ref T`, it is an error, and
the hint says `*r`. This holds in every position: operands of binary and
unary operators, `if`/`while` conditions, the collection of `for … in`, an
index, an argument for a non-`ref` parameter, a `return` for a non-`ref`
return type, `let x: T = r`, `x = r`, and the right-hand side of `*r = s`.

```noxy
let x: int = 10
let r: ref int = ref x

let y: int = r + 1     // ERROR: operand of '+' cannot be ref int: a ref is never read implicitly
                       //   hint: use '*r' to read the referenced value
let y: int = *r + 1    // OK: 11
let n: int = r         // ERROR: type mismatch in 'n' declaration: expected int, got ref int
let n: int = *r        // OK
if rb then … end       // ERROR (rb: ref bool) — use 'if *rb then'
for v in ra do … end   // ERROR (ra: ref int[]) — use 'for v in *ra do … end'
```

A parameter or slot typed `any`, and a native without a signature (`print`,
`to_str`), accept a `ref T` **as a value**: the reference travels. So
`print(r)`, `to_str(r)` and `f"{r}"` (which is `to_str(r)`) show `<ref …>`;
write `print(*r)` to see the value. `let v = r` (inferred) gives `v: ref T`.

A group of natives also rejects a `ref` argument — they take a value, never
a reference. `length`, `keys`, `slice`, `contains`, and `has_key` check
**argument 1** (the collection): the second argument of `contains`/`has_key`
is an element or a key, and a `ref` there is a legitimate search value
(`contains(ys, r)` over a `(ref int)[]` finds it by identity). The encoding,
serialization, and crypto natives check **every** argument: `json_dumps`,
`json_dumps_result`, `json_parse`, `base64_encode`, `base64_decode`, `hex`,
`hex_encode`, `hex_decode`, `base62_encode`, `base62_decode`, `to_bytes`,
`fmt`, `crypto_pbkdf2_sha256`, `crypto_aes256_gcm_encrypt`, and
`crypto_aes256_gcm_decrypt`. When the argument's type is known, the compiler
rejects it statically with the same `*r` hint (`argument 1 to 'length':
expected a value, got ref int[]`); when it arrives through `any`, the same
check happens at runtime (`length: argument 1 expected a value, got ref`,
hint `a ref is never read implicitly; use '*r'`). `print`, `to_str`,
f-strings, the other unsigned natives, and any `any` parameter or slot still
receive the ref as a value, as above.

#### R3. `*r` is the only read and the only write of the referenced value

```noxy
let v: int = *r     // read
*r = 20             // write: x is now 20
*r = *s             // copy the value s points to into x
```

`*x` where `x` has a KNOWN static type that is not a reference is a compile
error (`cannot dereference non-reference value of type int`; `cannot
dereference non-reference type int in assignment` for `*x = v`). When the
static type is `any` or unknown, `*x` compiles — `any` does hold references
(R2) — and the same check happens at runtime (`cannot dereference int`).
`*r` with `r == null` is a runtime error too: `cannot dereference null
reference` when reading, `cannot write through a null reference` when
writing. Iterating a `ref` reached through `any` (`for v in a`) is the same
refusal: `cannot iterate over a ref: a ref is never read implicitly`.

#### R4. `.` and `[]` go through the reference

`r.f`, `r[i]`, `r.f = v`, `r[i] = v`, `ref r.f`, `ref r[i]` are shortcuts
for `(*r).f` and so on, at any depth (`r.f.g`, `r[i].f`), and through an
`any` base at runtime. This is the language's one shortcut. The index itself
follows R2: `xs[ri]` with `ri: ref int` is an error — `xs[*ri]`.

```noxy
func insert(node: ref TreeNode, valor: int)
    if valor < node.valor then          // '.' goes through node
        if node.esquerda == null then   // the stored ref, compared to null (R7)
            let novo: TreeNode = TreeNode(valor, null, null)
            node.esquerda = ref novo    // rebind of the field (R6)
        else
            insert(node.esquerda, valor)  // the stored ref, passed as a value (R5)
        end
    end
end
```

#### R5. A reference is never created implicitly

A parameter, a constructor field, or a slot typed `ref T` accepts exactly:
`ref x` (R1); an expression whose static type is already `ref T` (a `ref`
variable, field, element, map entry, or a call returning `ref T`); or
`null`. Passing a plain `T` is an error with the hint `use 'ref x'`. The
rule is the same for user functions, typed `func` values, bare `func`,
struct constructors, generic instantiations, and builtins:

```noxy
func checkout(c: ref Cart) -> void … end
checkout(mine)          // ERROR: argument 1 to 'checkout': expected ref Cart, got Cart
                        //   hint: use 'ref mine'
checkout(ref mine)      // OK — and the call site shows that mine may change

append(xs, 1)           // ERROR — hint: use 'ref xs'
append(ref xs, 1)       // OK
pop(ref xs)             // OK
delete(ref m, "k")      // OK
json_loads(text, ref target)   // OK

func push(p: ref int[]) -> void
    append(p, 9)        // OK: p is already ref int[] — passed as is
end
```

An argument of type `any` is accepted at compile time; the runtime checks
the parameter mode (`function 'f' argument 1: expected ref int, got int`)
and, for a `ref T` parameter, the type of the target the reference points
to (`function 'f' argument 1: expected ref int, got ref string`); a `null`
target is forwarded and fails on the read (§4.2).

#### R6. Rebind is `=`, update is `*… =`

With `r: ref T`: `r = ref y` rebinds (`r` now points to `y`); `r = v` with
`v: T` is an error (hint: `*r = v`); `*r = ref y` is an error (hint: `r =
ref y` to rebind, or `*r = y` to write the value). The same holds for a
field, element, or map entry typed `ref T`: `x.next = ref n` rebinds;
`x.next = n` is an error. Rebinding a `ref` parameter changes only the
callee's reference, never the caller's.

#### R7. `==` and `!=`

Two references compare by **slot identity**; a reference compared with
`null` asks whether the reference itself is null; a reference compared with
a plain value is an error (R2) — write `*r == v`.

```noxy
let ra: ref int = ref a
let ra2: ref int = ref a
let rb: ref int = ref b   // b == a == 1
ra == ra2    // true  — same slot
ra == rb     // false — different slots
ra == null   // false — the reference is valid
*ra == 1     // true  — the values
ra == 1      // ERROR — hint: use '*ra'
```

`addr(ref x)` gives the identity as a printable value.

#### R8. `null` is a valid `ref T?` — and only there

A reference that may be absent is declared `ref T?` (a *nullable
reference*, §2.4): it can be stored, passed, returned, compared with `null`
and replaced by rebind. A bare `ref T` is never `null` — `let r: ref int =
null`, `f(null)` for `n: ref Node`, `Node(1, null)` for a field `next: ref
Node` are compile errors with the hint `declare it as 'ref T?' to allow
null`. Reading or writing through a `ref T?` (`*r`, `r.f`, `*r = v`) requires
a null test first (`'r' may be null; test it first`); inside `if r != null
then … end` the reference is `ref T`. The runtime errors `cannot dereference
null reference` / `cannot write through a null reference` remain only for
references that arrive through `any`.

`ref (T?)` is the other shape: a non-null reference to a slot whose *value*
may be null (`ref raiz` with `raiz: TreeNode?`); `*r` is `T?` and is tested
the same way. The two are distinct types and never convert into each other.

#### R9. Lifetime of a referenced local

`ref x` on a local promotes the slot of `x` to a heap cell (an upvalue). The
cell lives as long as any reference to it exists — including after the
function that declared `x` has returned. This is how nodes are allocated;
there is no `new`:

```noxy
let novo: Node = Node(v, null)   // a variable: `ref` needs an l-value (R1)
node.next = ref novo             // `novo` becomes a cell; it outlives this function
```

Cost: one cell allocation per referenced local, and from then on the variable
takes part in ownership counting; locals that are never referenced stay on
the stack. `==` between references compares these cells (R7). A closure that
captures a `ref` to a local and is handed to `spawn`/`spawn_task` shares the
cell between routines — coordinate, as for globals ([docs/concurrency.md](concurrency.md)).

#### R10. A reference into a container denotes a place

`ref x` on a **name** promotes that slot to a heap cell (R9). `ref` on a
**field, an index, or a map entry** — `ref p.x`, `ref a[i]`, `ref m[k]` —
cannot denote a cell: the referent lives inside a composite that copy-on-write
may duplicate. Such a reference denotes a **place**, and the path from the root
variable down to it is resolved **at each access**, not when the reference is
created:

```noxy
let arr: int[] = [1, 2, 3]
let r: ref int = ref arr[0]
let copia: int[] = arr        // copy AFTER the reference was taken
*r = 999
print(arr)                    // [999, 2, 3] — the write reaches the original
print(copia)                  // [1, 2, 3]   — the copy is independent
```

Writing through the reference unicizes every level of that path and stores each
clone back into its parent, exactly as the equivalent direct assignment
(`arr[0] = 999`) would. So a reference into a container costs what the direct
assignment costs, and value semantics hold no matter when the copy was made —
before or after the reference.

Because the path is resolved on access and not frozen, a reference stays valid
across a copy-on-write fork of anything above it. Two consequences follow from
the reference naming a *place* rather than an object:

```noxy
let arr: int[] = [1, 2, 3]
func f(r: ref int) -> void
    arr = [7, 7, 7]           // the root now holds a different array
    *r = 999                  // writes arr[0], which is that array's first slot
end
f(ref arr[0])
print(arr)                    // [999, 7, 7]
```

```noxy
let m: map[string, int] = {"a": 1}
func g(r: ref int) -> void
    delete(ref m, "a")
    *r = 999                  // Runtime error: reference target no longer exists
end
g(ref m["a"])
```

A place that has been removed is not silently recreated: writing through a
reference whose entry no longer exists is an error, in the spirit of the Zen —
*a failure that indicates a bug raises and stops the program*.

#### Diagnostics

| Situation | Message | Hint |
|---|---|---|
| `ref T` where `T` expected (R2) | `… expected T, got ref T` / `operand of '+' cannot be ref int: a ref is never read implicitly` | `use '*r' to read the referenced value` |
| `for x in r` | `cannot iterate over ref T[]: a ref is never read implicitly` | `use 'for x in *r'` |
| `xs[ri]` | `index cannot be ref int: a ref is never read implicitly` | `use '*ri' to read the referenced value` |
| `f(x)` for `ref T` param (R5) | `argument N to 'f': expected ref T, got T` | `use 'ref x'` |
| `append(xs, v)` | `argument 1 to 'append': expected ref T[], got T[]` | `use 'ref xs'` |
| `f(41)` for `ref T` param | `argument N to 'f': expected ref T, got int` | `bind the value to a variable and pass 'ref <name>'` |
| `ref r` with `r: ref T` (R1) | `'r' is already a reference` | `pass 'r' directly, without 'ref'` |
| `let q: ref ref int` | `SyntaxError: 'ref ref' is not a type` | `a reference is never taken to a reference` |
| `r = v` (R6) | `cannot assign T to ref T` | `use '*r = ...' to update the referenced value` |
| `*r = ref y` (R6) | `cannot assign ref T to T through '*r'` | `use 'r = ref y' to rebind the reference, or '*r = y' to write the value` |
| `r == v` (R7) | `cannot compare ref T with T: a ref is never implicitly dereferenced in '=='` | `use '*r' to compare the referenced value` |
| `*x`, `x: int` (R3, static) | `cannot dereference non-reference value of type int` (`... in assignment` for `*x = v`) | — |
| `*x`, `x: any` at runtime (R3) | `cannot dereference int` | — |
| `*r` with `r == null` at runtime (R3) | `cannot dereference null reference` (`cannot write through a null reference` for `*r = v`) | — |
| `for v in a`, `a: any` holding a ref, at runtime (R2) | `cannot iterate over a ref: a ref is never read implicitly` | `use '*r'` |
| `length(rx)`, `rx: ref int[]` (R2, static) | `argument 1 to 'length': expected a value, got ref int[]` | `use '*rx' to read the referenced value` |
| `json_dumps(rx)`, `rx: ref int[]` (R2, static) | `argument 1 to 'json_dumps': expected a value, got ref int[]` | `use '*rx' to read the referenced value` |
| `length(a)` through `any` at runtime (R2) | `length: argument 1 expected a value, got ref` | `a ref is never read implicitly; use '*r'` |
| `ref a.f` through `any`, slot already ref (runtime) | `slot 'f' already holds a reference` | `pass it directly, without 'ref'` |
| `*r = v` where the map entry `r` names was deleted (R10) | `reference target no longer exists` | — |

---

### 2.4 Nullable types (`T?`)

`T?` is the type "`T` or `null`". It is the **only** spelling of nullability,
and it is a suffix that binds looser than `ref` and than `[]`:

| Written | Meaning |
|---|---|
| `Node?` | a `Node` value, or `null` |
| `ref Node?` | a *nullable reference*: the reference itself may be `null` (§2.3 R8) |
| `ref (Node?)` | a non-null reference to a slot whose value may be `null` |
| `Node?[]` | an array of nullable nodes; `Node[]?` is a nullable array |
| `(func(int) -> int)?`, `(ref func() -> int)?` | callables need the parentheses — `func() -> int?` reads the `?` on the return type |

`T??`, `any?` and `void?` are syntax errors (`any` already admits `null`).

**The five rules** (the model is Kotlin's null safety, which Dart 2.12+
follows as well):

1. A bare `T` is never `null`; `T?` may be. Every position that today spells
   a type — `let`, parameter, return, field, element, map value, `chan`
   payload — takes either.
2. A struct or `ref` variable without an initializer is an error unless it
   is `T?` (§3).
3. `null` where a `T` is expected is a compile error, with the fix in the
   hint: `expected Point, got null` / `hint: declare it as 'Point?' to allow
   null`; for a parameter, `hint: declare the parameter as 'ref Node?' to
   accept null`. A `T?` where a `T` is expected is a compile error too:
   `expected Point, got Point?` / `hint: 'p' may be null; test it first`.
4. Reading *through* a `T?` without a test is a compile error: `p.x`, `*r`,
   `r.f`, `xs[i]`, `for v in xs`, `if b then` on a nullable operand all
   report `'p' may be null; test it first` / `hint: use 'if p != null then
   ... end'`. A nullable that is not a stable expression (`busca(xs, 1).x`)
   gets `bind it with 'let' and test for null`.
5. A null test **narrows**: inside the branch where the test proved the
   value present, the expression has type `T`.

```noxy
struct Node
    valor: int
    prox: Node?               // owned list: the chain ends
end

func busca(xs: Node[], k: int) -> Node?     // may not find: the type says so
    for n in xs do
        if n.valor == k then
            return n
        end
    end
    return null
end

let achado = busca(lista, 7)               // inferred: Node?
print(achado.valor)                        // ERROR: 'achado' may be null; test it first

if achado != null then
    print(achado.valor)                    // ✓ achado: Node in this branch
end

if achado == null then
    return -1
end
print(achado.valor)                        // ✓ the null branch returned, so achado: Node from here on

let total: int = 0
let atual: Node? = inicio
while atual != null do                     // ✓ the loop body sees atual: Node
    total = total + atual.valor
    atual = atual.prox                     // the assignment ends the narrowing for the rest of the body
end
```

**What narrows.** A *stable expression*: an identifier, `*r`, or a member
chain `a.b.c`. Facts come from `e != null` (true branch), `e == null` (false
branch, and after the `if` when the true branch ends in `return`, `break`,
`continue` or `exit`), the right operand of `&&`/`||` seeing the left one
(`p != null && p.x > 0`), the body of `while e != null do`, and `if r.ok then`
on an `errors.Result<T>` (which narrows `r.value`, §7). A `ref x` taken on a
narrowed `x: T?` is a `ref T`.

**What ends a narrowing.** An assignment to the expression or to a prefix of
its path (`p = q`, `no = …` for `no.prox`); a call, for a path whose root
is a `ref`-typed variable, a global, an upvalue, a captured local or a local
whose address was taken with `ref` — someone else may reach it during the
call; a write through a reference (`*r = v`, `r.f = v`), which drops every
member-path fact; and entering a loop whose body assigns the root. A path
rooted at a plain value local survives calls: value semantics (§2.2)
guarantee nobody outside the frame can reach it.

A call to a core builtin (§10: `print`, `eprint`, `to_str`, `length`, `fmt`,
`keys`, `slice`, `append`, `pop`, `delete`, `range`, …) that the program has
not shadowed, or to a struct constructor, ends **no** narrowing: those never
run Noxy code, so nothing can reach the root during the call (`json_loads` is
the exception — it writes through its `ref` argument and may leave `null` in
the root). That is why `f"{m['a']} {m['b']}"` (two `to_str` calls,
§9) and `print(m["a"])` followed by `print(m["b"])` keep a global `m`
narrowed, and a loop whose body only calls them keeps the fact too. A call
to a program function, `call_result`, `spawn_task`, `task_await` or a bare
`func` does run program code and ends it. That includes a call in the right
operand of `&&` (or of `||`, for the false branch): it runs *after* the test
in the left operand, so `if m != null && toca() then m["k"] end` with a
global `m` is an error (`'m' may be null: it was tested, but 'm' is a global
and a call in the condition ran after the test`), while `if toca() && m !=
null then` and `if m != null && length(m) > 0 then` keep the fact.

When a fact is lost, test again or bind first (`let n = no.prox` then `if n
!= null then … end`). The diagnostic says which happened: a read after the
fact was dropped by a call reports `'m' may be null: it was tested, but 'm'
is a global and a call came between the test and this use`, with the fix in
the hint (`test it again after the call, bind it first ('let v = m' before
the 'if') and use 'v', or move the code into a function`). A top-level `let`
is a global, so file-level code loses its facts at every call to a program
function; inside a `func main()` the same binding is a value local and
survives them. Facts never survive into a different function: a closure body
starts with none.

**Generics and modules.** A type parameter `T` may bind a nullable type
(`first(ps)` with `ps: Node?[]` returns `Node?`); it still cannot bind a
`ref` (§6.5). A field `value: T?` instantiated with `T = X?` is `X?`, not
`X??`. `T?` travels through module signatures and struct fields like any
other type.

**Runtime mirror.** The dynamic boundary (§4.2) enforces the same rule: a
`null` arriving through `any` into a `Point`, `ref int` or `int[]` slot is a
runtime error (`expected Point, got null` / `hint: declare the slot as
'Point?' to allow null`); into a `T?` slot it is accepted. `json_loads` fills
a JSON `null` only into a nullable field or element.

**Not narrowed by design.** Fields of a struct value read after a call
through a `ref` root, map lookups (`m["k"]`), array elements (`xs[i]`) and
call results are not stable expressions; bind them to a `let` first. `any`
is untouched by all of this: it holds `null` and every other value, and its
reads are checked at runtime as before.

---

## 3. Variable Declarations

```noxy
let name: type = value
let name = value          // type inferred from the initializer
```

Variables can be reassigned, but the new value **MUST** be of the same type as declared.

### Local type inference

When the annotation is omitted, the variable's declared type is the **static
type of the initializer**, fixed at the declaration exactly as if it had been
written. Inference is local and one-directional — from the right-hand side to
the binding, only in `let` — and it does not make the variable dynamic: the
type-stability rules of §2.0 apply unchanged.

```noxy
let n = 42                 // n: int
let name = "Noxy"          // name: string
let xs = [1, 2, 3]         // xs: int[]   (a dynamic array, not int[3])
let m = {"a": 1}           // m: map[string, int]
let p = Point(1, 2)        // p: Point
let r = ref n              // r: ref int (a borrow, like `let r: ref int = ref n`)
let y = id(5)              // y: int — the generic instance is inferred first (§6.2)

n = "text"                 // ERROR: type mismatch — n is int
```

Annotations stay mandatory where they are contract documentation — function
parameters and return types, struct fields — and where the initializer does
not have a single type of its own. Each of these is a compile-time error
(`cannot infer type for 'x' from its initializer: ...`) with a hint showing the
annotated form:

| Initializer | Why | Write instead |
|-------------|-----|---------------|
| `[]`, `{}` (also nested: `[[]]`, `{"a": []}`) | empty literal, no element/key/value type | `let xs: int[] = []`, `let m: map[string, int] = {}` |
| `null` (also `[null]`) | `null` is a value of the nullable types, not a type | `let p: Point = null` |
| a call to a `void` function | there is no value to bind | return a value, or annotate |
| an expression of type `any` | the dynamic boundary must be spelled out (`any` *nested* in a type, such as `map[string, any]`, is an ordinary type and is inferred faithfully) | `let v: any = parse(s)` |
| a name whose type is not known yet (`let a = b` with `b` declared later; also a later un-annotated global read inside the body of a generic function that a top-level `let y = id(...)` instantiates) | no static type at that point — top-level inferred `let`s are typed in file order | annotate, or reorder |
| a builtin outside the typed core set (§10) — `json_parse`, `task_await`, `make_chan`, ... | its result is `any` or has no static type | annotate (`let v: any = json_parse(s)`) |

A member reached through a namespace import — `m.f(...)`, `m.x`, `m.T(...)`
after `use m` — has the type the module declared for it, translated to the
program's view exactly as a field of a module struct is (§11), so `let v =
m.roll(6)` binds `v: int` and `let p = vec.norm(v)` binds `p: vec.V`. The
value is fully typed even when the program has no way to *write* its type
(a struct of a module it never imported, and not reachable by any visible
alias): with `use mid select mkv` alone — `mid.nx` itself does `use base
select *` without declaring `V` — `let v = mkv()` binds `v` to the
declaration of `V` in `base.nx`, and `v.x`'s type is checked (`let s: string
= v.x` is a compile error, `expected string, got int`) exactly as for a
locally declared struct; a field name that `V` does not declare is still a
runtime error (`undefined property 'y'`), as for any struct value. A
mismatch on `v` itself prints the canonical path (`expected string, got
base.V`). Only a written annotation needs a name (§11, *Unknown type
names*).

The core builtins whose result type never varies — `length`, `to_str`,
`to_int`, `to_float`, `to_bytes`, `type`, `input`, `fmt`, `hex`,
`hex_encode`, `hex_decode`, `ord`, `contains`, `has_key`, `json_dumps`,
plus `keys(map[K, V]) -> K[]` and `slice` (same type as its first argument)
— have a static return type (§10), so `let n = length(xs)` binds `n: int`.
That type is checked in every position, not only in `let`: `let s: string =
length(xs)` is a compile-time error.

A `ref` initializer binds a **borrow**, exactly as the annotated form does:
`let v = r` with `r: ref T` gives `v: ref T` (the same slot, no copy). To copy
the value out of a reference, annotate and read: `let v: T = *r` — `let v: T
= r` is an error (§2.3 R2).

`let x` with neither annotation nor initializer is a syntax error (there is
nothing to infer); `let x: T` without an initializer keeps the default-value
rule below. Inferred declarations are ordinary declarations in every other
respect: a top-level `let x = 10` is visible, typed `int`, to every function in
the file (including ones declared before it), is exported by the module with
that type, and the REPL infers line by line (`>>> let x = 10`).

### Declaration without an initializer

`let name: type` (no `= value`) is allowed only for types that have a
**default value**, and the variable starts with it:

| Type | Default |
|------|---------|
| `int`, `float`, `bool`, `string`, `bytes` | `0`, `0.0`, `false`, `""`, `b""` |
| `T[]`, `map[K, V]` | `[]`, `{}` (empty) |
| `T[N]` | `N` copies of `T`'s default — so `T` must have one |
| `T?`, `any` | `null` (the types that accept `null`, §2.4) |

Struct types, `ref T`, `chan T` and function types (`func`, `func(A) -> R`)
have **no default**: `null` is not a value of those types (`let p: Point =
null`, `let c: chan int = null` are type errors), so a declaration without
an initializer is a compile-time error — give the value up front, or declare
the slot nullable when "not yet" is a legitimate state:

```noxy
let p: Point              // ERROR: variable 'p' needs an initializer: Point has no default value
                          //   hint: write 'let p: Point = ...' or declare it as 'Point?'
let q: Point?             // ✓ null
let r: ref int?           // ✓ null
let n: int                // ✓ 0
let cs: (chan int)[]      // ✓ [] — the array is empty, no element default needed
let c: chan int           // ERROR: variable 'c' needs an initializer: chan int has no default value
let f: func(int) -> int   // ERROR: same rule — func(int) -> int has no default value
let ch: chan int = make_chan(1)   // ✓
```

### Redeclaration vs. reassignment

`let` introduces a new binding; `=` updates an existing one. Declaring a
name that already exists **in the same scope** is a compile-time error —
assignment is the only way to change a variable's value, and no sequence
of statements can change its type:

```noxy
let x: int = 1
let x: int = 2       // ERROR: variable 'x' redeclared in this scope
let x: string = "s"  // ERROR: same rule — redeclaration can never change the type
x = 2                // ✓ OK — assignment updates the value
```

The REPL applies the same rule: a session behaves like a single file typed
line by line, so re-declaring a name from an earlier line is rejected —
assign to it instead. A rejected line does not claim its name: after an
error you may still `let` that name.

**One global namespace.** `let`, `func`, `struct` and imported names share
the global scope. Declaring a name that another kind already holds is the
same error, naming the earlier declaration: `'x' redeclared in this scope
(previous declaration as function at line 1)`. This covers `let x` over
`func x`, `struct P` over `func P`, and a `let` or `func` over a name
brought in by `use m`, `use m select a` or `use m select *`. Importing the
same name twice is not a collision. In the REPL the only relaxation is
redefining a *function* with another function on a later line — the way one
iterates on a definition; every other cross-kind collision is rejected as in
a file.

**Every global name resolves at compile time.** Reading or assigning a name
that is neither declared in the program (anywhere — a function body may use
a global declared later in the file), nor imported, nor provided by the
runtime is a compile error, even inside a branch that never runs:

```noxy
let cond: bool = false
if cond then
    print(typo_global)   // ERROR: undefined global 'typo_global'
end                      //   hint: declare it with 'let typo_global = ...' or check the spelling
i = 0                    // ERROR: cannot assign to undeclared name 'i'
```

The set of runtime-provided names (the built-in natives, extension and
plugin exports) comes from the embedder: the CLI, the REPL and the module
loader seed the compiler with the names the VM defines, so `print` or
`io_open` are known without being declared. A compiler used standalone,
without a VM, does not check global names.

### Shadowing in inner scopes

A `let` in an **inner** scope (a block body, or a function body shadowing a
parameter) creates a distinct variable that hides the outer one until the
block ends. The outer variable is untouched:

```noxy
let x: int = 99
if x > 0 then
    let x: string = "inner"  // ✓ OK: new variable, dies at 'end'
end
print(x)                     // 99
```

---

## 4. Functions

### 4.1 Definition

```noxy
func name(param1: type1, param2: type2) -> returnType
    // body
    return value
end

// Anonymous Function / Closure
let adder: func = func(x: int) -> int
    return x + 1
end

// Nested Functions
func makeAdder(x: int) -> func
    return func(y: int) -> int
        return x + y // Captures 'x'
    end
end
```

### 4.2 Function Types

Every function value is callable. Noxy provides two ways to describe how precisely it may be called.

An **exact function type** records parameter and return types:

```noxy
func(int, int) -> int
func(string) -> void
func(ref int) -> void

let exact: func(int) -> int = double

func apply(f: func(int) -> int, value: int) -> int
    return f(value)
end
```

Calls through exact types are checked during compilation: arity, argument types, `ref` addressability, and return types must match. Exact signatures are invariant; parameter count, parameter types, `ref` modifiers, and return types must be identical. `null` satisfies `ref`, struct, and `any` contracts, but not primitive value or callable contracts. An omitted return annotation on a function declaration or literal means `void`.

A `ref T` parameter takes a reference and nothing else — `ref x`, a value
that already has type `ref T`, or `null` (§2.3 R5). That is true for exact
signatures, bare `func`, natives, and plugins alike; there is no contextual
conversion at any boundary:

```noxy
let value: int = 10
double_it(ref value)   // OK
double_it(value)       // ERROR — hint: use 'ref value'

func append_node(node: ref Node, valor: int)
    if node.proximo == null then
        let novo: Node = Node(valor, null)   // a variable: `ref` needs an l-value
        node.proximo = ref novo              // rebind of the owner's field
    else
        append_node(node.proximo, valor)     // the stored reference, passed as a value
    end
end
```

A slot declared `ref T` always holds a reference or `null`, and the runtime
never wraps anything else. Through a base typed `any`, `a.proximo` reads the
stored reference (or `null`) exactly as the typed base does; `ref a.proximo`
on such a slot is the runtime error `slot 'proximo' already holds a reference`.

Bare `func` is the **dynamic callable type**. It guarantees only that the value is callable:

```noxy
let dynamic: func = exact       // exact-to-dynamic widening is valid
let exact_again: func(int) -> int = dynamic // ERROR: no implicit narrowing
```

Calls through bare `func` are checked by the runtime because their arity and result type are not statically known. Their compile-time result is `any`, which crosses into a typed slot under the runtime check described at the end of this section — a function returning their result may declare the concrete type, or `any`. Explicit `ref` arguments remain references across this dynamic boundary. This keeps dynamic callbacks, decorators, handlers, and heterogeneous callable collections available without pretending their results are statically known:

```noxy
let callbacks: func[] = [no_arguments, two_arguments]
```

A reference argument is always written `ref value` (§2.3 R5); through bare
`func` the runtime checks the mode. The same rule applies to untyped
native primitives, plugins without signatures, and dynamic module members.

Use parentheses for an array whose elements are exact functions. Without parentheses, the array belongs to the return type:

```noxy
let transforms: (func(int) -> int)[] = [double, increment]
let factory: func(int) -> int[] = make_list   // one function returning int[]
```

Exact function types may also appear in parameters, returns, struct fields, map values, channels, and references. `any`, native functions, plugins, and untyped module exports remain dynamic boundaries and retain runtime validation; an unknown native value cannot be implicitly narrowed to an exact function type.

**The dynamic boundary is checked at runtime, in every position.** A value
whose static type is `any` — an `any`-typed variable, field or parameter,
the result of a function declared `-> any`, of `json_parse`, `task_await` or
a bare `func` call — is accepted wherever a typed slot is expected: an
annotated `let`, an assignment (`x = v`, `s.f = v`, `xs[i] = v`), an
argument of an exact signature, a `return`. The runtime checks the value
against the slot's type at that point and names both sides on mismatch:
`expected int, got string`, `expected map[string, any][], got int[]`,
`expected Point, got null` (§2.4). Typed code pays nothing — the check is
emitted only where the static type is `any`. A wrapper around an
`any`-returning function therefore returns it directly:

```noxy
func itens() -> any                       // e.g. a parsed JSON payload
    return json_parse(corpo)
end

func scan() -> map[string, any][]
    return itens()                        // checked here, at runtime
end
```

The standard library honours the `any` contract on its own side: a wrapper
whose native reports failure with `null` says so in its signature —
`time.parse(s) -> DateTime?`, `time.parse_date(s) -> DateTime?`,
`sqlite.prepare(db, sql) -> Statement?`, `crypto.aes256_gcm_decrypt(key,
data) -> bytes?` — so `let dt = parse(s)` imported with `select` has the
static type `DateTime?` and is tested with `if dt != null then`. A native
that rejects an *argument* (`crypto.random_bytes(0)`, a 5-byte AES-256 key,
a value that is not a socket) raises a runtime error instead: `null` under a
`-> T` wrapper is never a way of saying "bad argument".

Three limits. `any` crosses only at the *top* of a type: element, key,
value, channel payload and `ref` target are invariant, so `any[]` is not an
`int[]`, `map[string, any]` is not a `map[string, int]`, and `ref a` with
`a: any` is a `ref any`, never a `ref int`. An `any` value that *carries* a
reference (R2) may fill a `ref T` slot or parameter — R5's dynamic
boundary — and there the target is checked, not only the mode: `inc(a)`
with `inc(r: ref int)` and `a` holding `ref s` (`s: string`) is `function
'inc' argument 1: expected ref int, got ref string`; `let r: ref int = a`
is `expected ref int, got ref string`. A `null` target is forwarded as
before and fails on the read. And an exact function type is never a target: `any` is not
narrowed to `func(int) -> int`, nor to a type containing one
(`(func(int) -> int)[]`) — the rule above, no implicit narrowing to an exact
signature, holds at every boundary.

A value of *unknown* static type — an untyped native, a plugin, a module
member called through a namespace (`m.f()`) — is accepted as before and is
not checked by this guard: the natives declare no return contract yet, and
checking every wrapper call measured at +35–55 % per call. Only a composite
slot (array, map, chan) carries its runtime marker in every position.

### 4.3 Parameter Passing Semantics (CRITICAL)

Noxy uses **Pass-by-Value** by default. Primitive values are copied directly. Composite values (arrays, maps, and structs) behave as independent deep copies at any depth through value fields, implemented with copy-on-write (see §2.2); a field declared `ref T` is the one edge a copy shares (§2.2 rule 6).

#### Pass-by-Value (Default)

When a composite value is passed to a parameter without `ref`, the function's view is independent of the caller's through every value field — mutating it at any depth never affects the caller:

```noxy
func modify(arr: int[]) -> void
    append(ref arr, 999) // Callee's value only
end

let list: int[] = [1, 2, 3]
modify(list)
// list is still [1, 2, 3]
```

Independence is deep — nested composites do not leak either:

```noxy
struct Box
    values: int[]
end

func mutate_nested(box: Box) -> void
    box.values[0] = 99 // Mutates the callee's independent value
end

let values: int[] = [1, 2]
let box: Box = Box(values)

mutate_nested(box)
// box.values is still [1, 2]
// values is still [1, 2]
```

The boundary of that independence is a field declared `ref`. The sharing is
written in the struct declaration, not in the signature or at the call site:

```noxy
struct Node
    value: int
    next: ref Node
end

struct Holder
    inner: ref Node      // Holder is ref-carrying (§2.2 rule 6)
end

func touch(h: Holder) -> void
    h.inner.value = 777  // writes through the shared edge
end

let target: Node = Node(1, null)
let holder: Holder = Holder(ref target)

touch(holder)
// target.value is now 777: the callee's copy of `holder` holds the same
// `ref` — `Holder` declared that. With `inner: Node` instead, target.value
// would still be 1.
```

#### Pass-by-Reference (`ref`)
To share the caller's value and let the function mutate it, use `ref` in the parameter type and `ref` at the call site — the signature and the call both say so.

```noxy
func modify(arr: ref int[]) -> void
    append(arr, 999) // Modifies the ORIGINAL list
end

let list: int[] = [1, 2, 3]
modify(ref list)
// list is now [1, 2, 3, 999]
```

This applies to Structs and Maps as well.

---

## 5. Structs

```noxy
struct Point
    x: int,
    y: int
end

// Constructor
let p: Point = Point(10, 20)

// Field Access
p.x = 15
```

A struct is **nominal and its fields are fixed** by the declaration. Reading
or writing a name that is not declared is an error — at compile time on a
typed base (`unknown field`), and at runtime through a dynamic one
(`undefined property 'zzz'` for `let d: any = p` followed by `d.zzz` **or**
`d.zzz = 1`). No path adds a field to an instance.

### Self-Reference

A struct may contain its own type in two ways, and they mean different things.

**Ownership — a field without `ref`.** A struct-typed field declared `T?`
accepts `null` (§2.4), so a recursive structure with a single owner per node
— a list, a tree — needs no `ref` in its declaration: the chain ends in a
`null`. The children are values: copying a node copies the whole subtree
(copy-on-write, so the copy is lazy), and a callee that receives a node by
value cannot reach the caller's tree. In-place mutation goes through a `ref`
to the *slot* — `ref (TreeNode?)`, a reference to a place that may hold null
(§2.3 R1, R8, R10); `if *node == null then … return` narrows `*node` for the
reads that follow:

```noxy
struct TreeNode
    valor: int
    esquerda: TreeNode?     // owned; null where the tree ends
    direita: TreeNode?
end

func insert(node: ref (TreeNode?), v: int) -> void
    if *node == null then
        *node = TreeNode(v, null, null)
        return
    end
    if v < node.valor then
        insert(ref node.esquerda, v)
    else
        insert(ref node.direita, v)
    end
end

let raiz: TreeNode? = null
insert(ref raiz, 50)
insert(ref raiz, 30)
let copia: TreeNode? = raiz
if copia != null && copia.esquerda != null then
    copia.esquerda.valor = 999    // raiz.esquerda.valor is still 30
end
```

A fact about `*node` does not survive a call whose root is a `ref` (§2.4):
in `1 + count(ref node.esquerda) + count(ref node.direita)` the second
borrow needs a fresh test — bind the first result to a `let` and test again.

`noxy_examples/bst_owned.nx` is the reference program for this idiom. Its
cost today is the nested borrow (`ref node.esquerda` inside a recursion) —
tracked as issue #93; prefer it whenever the structure has one owner per node.

**Sharing — a field declared `ref`.** When a node must be reachable from more
than one place — a node with two parents, a doubly linked list (`prev`), a
parent pointer, a graph with shared edges — the field is declared `ref` and
the struct becomes *ref-carrying* (§2.2 rule 6):

```noxy
struct Node
    value: int
    next: ref Node?         // shared; null where the list ends
end
```

A `ref` field is filled by rebinding it to a variable (`let novo: Node = ...;
node.next = ref novo`) or — when declared `ref Node?` — cleared with `null`;
assigning a raw `Node` to it is a compile error whether `node` is a `Node`
or a `ref Node` (§2.3, §4.2). Reading `node.next.value` needs `if node.next
!= null then` first (§2.4). The
sharing is the point of such a field, and it travels with every copy of the
container: `let b = a`, `f(a)` with `f(x: Node)`, `arr[i]`, `chan_send` all
hand out a value whose `next` reaches the *same* node — the copy is
independent through its value fields and shares through its `ref` fields.
That is declared once, in the struct, and not repeated at the call site.

A struct may also reference itself through an **array field without `ref`**:
value semantics without `ref` cannot form a cycle, so no `ref` is needed to
keep construction well-founded.

```noxy
struct Failure
    kind: string
    causes: Failure[]
end
```

---

## 6. Generics

Noxy supports generic functions and structs, monomorphized at compile time:
each instantiation (`Stack<int>`, `Stack<string>`) is compiled into its own
concrete, fully-typed code. There is no runtime type erasure and no runtime
overhead compared to code written by hand for a single type — this is a
compile-time feature only.

### 6.1 Declaring generic functions and structs

Type parameters are listed in `<>` right after the name and can be used
anywhere a type is expected — parameters, return type, fields, and the body:

```noxy
func first<T>(arr: T[]) -> T
    return arr[0]
end

struct Stack<T>
    items: T[]
end
```

A generic type only appears in **type position** — annotations, fields,
return types (`Stack<int>`, `Node<string>`, `Stack<Stack<int>>`). There is no
explicit instantiation syntax in expression position; `first<int>(x)` does
not exist as syntax, which keeps `<`/`>` unambiguous with the comparison
operators.

Structs can reference themselves through a type parameter, exactly like the
non-generic self-reference in §5:

```noxy
struct Node<T>
    value: T
    next: ref Node<T>
end

let n2: Node<int> = Node(2, null)
let n1: Node<int> = Node(1, ref n2)
print(n1.next.value) // 2
```

### 6.2 Instantiation is always by inference

There is no explicit instantiation syntax. Every use of a generic function or
struct infers its type parameters from context:

```noxy
struct Stack<T>
    items: T[]
end

func push<T>(s: ref Stack<T>, item: T)
    append(ref s.items, item)
end

func peek<T>(s: Stack<T>) -> T
    return s.items[length(s.items) - 1]
end

let ints: Stack<int> = Stack([])   // T from the `let` annotation — the
                                    // constructor argument ([]) is empty and
                                    // carries no type information by itself
push(ref ints, 10)
push(ref ints, 20)
print(peek(ints)) // 20
```

This is the one place where an annotated `let` carries information the
arguments do not: with an inferred `let` (§3, `let ints = Stack([])`) there
is nothing to unify `T` against, and the call is rejected with the usual
"could not infer T — annotate the type" error. Once the arguments pin `T`
(`let ints = Stack([1, 2])`), the inferred `let` works like any other.

The declared return type of the enclosing function is the same kind of
anchor: a generic call in `return` position unifies its return type against
the annotation, exactly as an annotated `let` does. Arguments remain the
primary anchor; the annotation only resolves what the arguments leave open:

```noxy
func vazia<T>() -> T[]
    let xs: T[] = []
    return xs
end

func prepara() -> int[]
    return vazia() // T = int, from the enclosing function's return type
end

func nova() -> Stack<int>
    return Stack([]) // the constructor argument ([]) is empty; the
                      // return annotation supplies T = int
end
```

`Stack<int>` and `Stack<string>` are distinct nominal struct types;
using one where the other is expected is a compile-time type error, the same
as any other struct mismatch.

Nested instantiation resolves inside-out — `Caixa<int>` is instantiated
before `Caixa<Caixa<int>>` needs it:

```noxy
struct Caixa<T>
    valor: T
end

let dupla: Caixa<Caixa<int>> = Caixa(Caixa(9))
dupla.valor.valor = dupla.valor.valor + 1
print(dupla.valor.valor) // 10
```

Generic structs keep Noxy's ordinary copy-on-write value semantics — a copy of
`Caixa<Caixa<int>>` never mutates the original, at any depth:

```noxy
let outra: Caixa<Caixa<int>> = dupla
outra.valor.valor = 100
print(dupla.valor.valor) // still 10 — the copy does not reach the original
```

### 6.3 Generic functions as values (target-typing)

A generic function can be used as a plain function value — assigned,
returned, stored in an array, passed as an argument — as long as the
surrounding context gives it a **concrete target type**. Instantiation
happens at the point the generic becomes a value:

```noxy
func identity<T>(x: T) -> T
    return x
end

// `let` annotation is the target
let f: func(int) -> int = identity
print(f(5)) // 5

// return type of the enclosing function is the target
func escolhe() -> func(int) -> int
    return identity
end

// array element type is the target (parentheses required — see §4.2)
let fs: (func(int) -> int)[] = [identity]
```

When a generic function is passed as an argument to another generic function,
Noxy unifies both signatures together: the non-generic arguments resolve
first, and the resulting (possibly still partial) expected type is unified
against the argument function's own signature, propagating bindings in both
directions:

```noxy
func aplica<A, B>(arr: A[], fn: func(A) -> B) -> B[]
    let out: B[] = []
    for item in arr do
        append(ref out, fn(item))
    end
    return out
end

let nums: int[] = [1, 2, 3]
let mesmos: int[] = aplica(nums, identity) // A=int from `nums`, then
                                             // func(A)->B unified against
                                             // identity's func(T)->T gives B=int
```

A generic function cannot become a value where the compiler has no concrete
type to instantiate against — a bare `func` annotation, `any`, or a chain of
generics with nothing anchoring the middle. These are exactly the positions
where an ordinary function value would already lose static checking:

```noxy
let g: func = identity   // ERROR: 'identity' needs a concrete type — annotate
                          // the full signature or call it directly
```

### 6.4 Cross-module rules

A template's free identifiers — helper functions, other generics — must
resolve inside the module that defines it. Because the instance is compiled
in the importer's context, the importer must also have those dependencies
visible:

| Import form | Dependencies visible? | Result |
|---|---|---|
| `use m select *` | yes — everything enters the importer's globals | always works |
| `use m select f` (selective) | only if imported together | actionable error: *add it to the select list or use `select *`* |
| `use m` (namespace) | n/a | calling the template via `m.f(...)` is a compile-time error |

```noxy
// colecoes.nx
func ajuda(x: int) -> int
    return x + 1
end
func processa<T>(arr: T[]) -> int
    return ajuda(length(arr))
end
```

```noxy
// main.nx — selective import missing the dependency
use colecoes select processa
let ns: int[] = [1]
processa(ns)   // ERROR: 'processa<...>' needs 'ajuda' from 'colecoes' —
               // add it to the select list or use `select *`

// fixed: import the dependency alongside the template
use colecoes select processa, ajuda
let ns: int[] = [1, 2]
processa(ns)   // OK
```

Templates themselves are **not accessible through the namespace form**: since
a template never exists as a runtime value, member access cannot resolve it.

```noxy
use colecoes
let nums: int[] = [1]
colecoes.processa(nums) // ERROR: generic template 'processa' is not
                         // accessible via namespace access — use `select`
```

Imported names carry their **declared type** to the importer, so a generic
call can infer its type parameters from data or functions defined in another
module:

```noxy
// dados.nx
let numeros: int[] = [5, 6]
```

```noxy
// main.nx
use dados select numeros
func primeiro<T>(arr: T[]) -> T
    return arr[0]
end
print(primeiro(numeros)) // 5 — T inferred from the imported `numeros`
```

> Imported names now carry their real declared type to the importer instead
> of an erased one. Existing cross-module code that relied on the erased type
> being permissive can start failing to compile — see the 0.7.0 entry in
> `CHANGELOG.md` for the migration note.

### 6.5 v1 limitations

- **No constraints.** A type parameter `T` is unconstrained; the body is
  checked **per instantiation**. `soma<T>(a: T, b: T) -> T` with `a + b` in
  the body compiles fine when called with `int`, and fails — pointing at the
  call site that produced the failing instantiation — when called with a
  struct that has no `+` operator:

  ```noxy
  struct Ponto
      x: int
  end
  func soma<T>(a: T, b: T) -> T
      return a + b
  end
  soma(1, 2)                    // OK — soma<int>
  soma(Ponto(1), Ponto(2))      // ERROR: soma<Ponto> (instantiated at this
                                 // line): operator '+' is not defined for Ponto
  ```

- **Top level only.** A `func` or `struct` declared with type parameters
  cannot be nested inside a function body.

- **`T` cannot bind a `ref` type** (`ref X` or `ref X?`). Passing a `ref`
  value where `T` would bind to it is a compile-time error; declare the
  parameter as `ref T` instead when the generic needs to receive a
  reference. `T` **may** bind a nullable value type: `first(ps)` with
  `ps: Node?[]` returns `Node?`, and a field `value: T?` instantiated with
  `T = X?` is `X?` (§2.4).

  ```noxy
  func identity<T>(x: T) -> T
      return x
  end
  let r: ref int? = null
  let v: ref int? = identity(r)  // ERROR: T cannot be a ref type

  func identity_ref<T>(x: ref T) -> ref T  // idiomatic form: `ref` is
      return x                              // explicit, T binds to int
  end
  ```

- **`func` and `any` are not valid instantiation targets**, as shown in §6.3
  — Noxy never falls back to instantiating a generic implicitly as `any`.
  Type inference that cannot resolve a type parameter is always a
  compile-time error, never a silent `any`; this includes trying to infer a
  type solely from a `null` argument (`identity(null)` with no other anchor
  is a compile-time error too).

A generic struct instance is an ordinary struct value everywhere else in the
language — no special-casing needed for f-strings, `json_dumps`, or as a
channel payload sent and received with the `when`/`case` construct described
below:

```noxy
struct Caixa<T>
    valor: T
end
let c: Caixa<int> = Caixa(7)
let ch: any = make_chan(1)
chan_send(ch, c)
let recebida: Caixa<int>
when
    case bound = chan_recv(ch) then
        recebida = bound
    default
        recebida = Caixa(-1)
end
print(recebida.valor * 6) // 42 — the whole Caixa<int> traveled through the
                           // channel, not just its field
```

> **Note:** `map` is a type keyword (`map[K, V]`), so it cannot be used as the
> name of a generic function. The standard idiom, used by the `collections`
> module, is to call the transformation function `map_arr` instead:
> `func map_arr<A, B>(arr: A[], fn: func(A) -> B) -> B[]`.

> **Cosmetic note:** printing a generic struct instance shows its qualified
> name, e.g. `<main::Caixa<int> instance>` instead of `<Caixa instance>`.
> This is documented v1 behavior, not a bug.

---

## 7. Control Flow

### If-Then-Else
```noxy
if condition then
    // ...
elif condition2 then
    // ...
else
    // ...
end
```

A whole `if` may be written on a single line, including a bare `return`:

```noxy
func abs(n: int) -> int
    if n >= 0 then return n end
    return -n
end
```

#### The condition must be `bool`

There are no truthy/falsy values in Noxy. The condition of `if`, `elif` and
`while` — and the operands of `!`, `&&`, `||` — must be `bool`. When the static
type is known and is not `bool`, the program is rejected at compile time:

```text
[line 3] condition must be bool, got int
  hint: use an explicit comparison, e.g. 'x != 0', 'x != ""', 'x != null' or 'length(x) > 0'
```

Write the comparison explicitly:

| Instead of | Write |
|-----------|-------|
| `if n then` (`int`/`float`) | `if n != 0 then` |
| `if s then` (`string`) | `if s != "" then` |
| `if p then` (struct, array, map, `ref`) | `if p != null then` |
| `if xs then` (collection, meaning "not empty") | `if length(xs) > 0 then` |

A condition of type `ref bool` is an error — write `if *rb then` (§2.3 R2).
`&&`/`||` follow the same rule.

For a value whose static type is `any`, the check happens at runtime:
`condition must be bool, got int`.

### While Loop
```noxy
while condition do
    // ...
end
```

`break` leaves the innermost loop; `continue` skips to its next iteration —
in a `while` it re-evaluates the condition, in a `for` it advances to the next
element. Both discard the locals declared in the loop body, closing the box of
any local a closure captured, so a closure built in one iteration keeps that
iteration's values. `break` or `continue` outside a loop is a compile-time
error.

```noxy
let i: int = 0
while i < 10 do
    i = i + 1
    if i % 2 == 0 then continue end
    if i > 7 then break end
    print(i)          // 1, 3, 5, 7
end
```

### For Loop
Use `for ... in` to iterate over collections (arrays or maps).

**Arrays** (Iterates over values):
```noxy
for item in array do
    print(item)
end
```

**Counting loops** use the `range` builtin (§10), which returns an `int[]`
with Python semantics — no import needed:
```noxy
for i in range(3) do            // 0 1 2
    print(i)
end
for n in range(10, 0, -3) do    // 10 7 4 1
    print(n)
end
```

**Maps** (Iterates over keys):
```noxy
for key in map do
    print(map[key])
end
```

**Strings** (Iterates over characters):
```noxy
for char in "hello" do
    print(char)
end
```

The loop variable is **scoped to the loop**: it is created by the `for`,
may shadow an outer variable of the same name (left untouched), and no
longer exists after `end`. It is also **rebound from the collection on
every iteration** — assigning to it inside the body is allowed but never
affects the sequence:

```noxy
for i in [1, 2, 3] do
    i = 5        // allowed; next iteration rebinds i from the collection
end
print(i)         // ERROR: 'i' is not defined here
```

### Defer and deterministic cleanup

`defer` registers a call to run when its containing call frame exits. Its
operand must be a real call; arbitrary expressions and non-call operations
such as `defer addr(ref value)` are compile-time errors.

```noxy
let file: io.File = io.open(path, "w")
defer io.close(file)
```

The callee and every argument are evaluated immediately, from left to right,
when the `defer` statement executes. An evaluation or registration error does
not register that call, although calls registered earlier in the frame still
run. For typed Noxy functions, non-`ref` arguments are captured **by value** at
registration time (copy-on-write): later mutations by the enclosing frame are
not observed by the deferred call. Signed natives keep an eager copy at
registration, since native bodies mutate outside the bytecode's
copy-on-write. `ref` parameters retain their reference. Legacy untyped
natives retain values using their existing dynamic calling convention because
they expose no parameter-mode metadata. Struct constructors retain evaluated
field values directly, matching their existing constructor semantics.

Deferred calls execute once in last-in, first-out (LIFO) order for each frame.
A `defer` inside a loop registers once per executed iteration and belongs to
the containing frame, not to the loop or block. The same frame rule applies to
ordinary functions, the main script, module initialization, and functions run
by detached `spawn`. Deferred calls run on explicit and implicit returns, on
successful script or module completion, and while unwinding runtime errors.
Nested calls finish their own deferred work before the owning frame continues
with its next older entry.

An ordinary return value from a deferred call is discarded, including an
error-shaped Noxy result value. A runtime failure is different: the original
runtime error remains primary, every observable deferred failure is collected
in LIFO execution order with its registration location, and older deferred
calls continue to run. Nested cleanup and module failures remain nested,
structured causes rather than being flattened into one message. If cleanup is
the first failure, it converts an otherwise successful return into a runtime
error.

Resource cleanup uses the existing explicit close APIs. Register dependent
resources after their owners so LIFO order closes the dependent first; for
example, register `sqlite.close(db)` before `sqlite.finalize(statement)`.
Current file, network, and SQLite close builtins suppress some underlying
Go/operating-system close errors and return success-compatible Noxy values.
Those suppressed errors cannot participate in defer aggregation; only runtime
errors observable from the deferred call are aggregated. Likewise, the
ordinary result from `io.close_result(...)` is discarded when deferred.

### Errors: raise for bugs, results for data

Noxy has two failure channels, and they are not interchangeable.

A **runtime error** means the program is wrong: an impossible conversion of a
value the program itself produced, an out-of-range index, a violated native
contract. It unwinds call frames, runs deferred calls along the way, and —
unobserved — terminates the script with a diagnostic.

A **`Result<T>`** means the input was allowed to be bad: untrusted text,
user input, wire data. Failure is an expected outcome, so it is data — one
generic struct, declared in the `errors` module, that every operation with
an expected failure returns:

```noxy
struct Failure
    kind: string       // "runtime" | "panic"; "" in the success sentinel
    message: string
    stack: string
    causes: Failure[]  // deferred-call failures aggregated during unwinding
end

struct Result<T>
    ok: bool
    value: T?          // T on success, null on failure
    failure: Failure   // an empty Failure on success — never null
end
```

Both fields always exist and `ok` says which one is meaningful. `if r.ok
then` narrows `r.value` from `T?` to `T` inside the branch (§2.4), so the
happy path reads the value without a second test; the `else` branch reads
`r.failure.message`. The constructors are `Ok(v)`, `Err(message)` and
`Fail(failure)` — capitalized, like the type, so that `let ok: bool = …` in
a program that imports the standard library never collides with them.
Because Noxy functions return a single value, `Result<T>` occupies the place
of Go's `value, err` pair.

API design rule: an operation whose failure indicates a caller bug raises; an
operation whose failure is an expected outcome of untrusted data returns a
`Result<T>`. When both kinds of caller exist, provide the raising form and a
`_result` twin: `to_int` / `to_int_result` (`Result<int>`), `io.close` /
`io.close_result` (`Result<bool>`), `io.write` / `io.write_result`
(`Result<int>`, the bytes written), `json.dumps_result` (`Result<string>`).

Two boundaries convert the first channel into the second, and they are the
only two: the supervised-task boundary (`spawn_task` / `task_await`), and the
synchronous `call_result` described next. Advisory hints such as "use
to_int_result to handle failure" belong to the top-level fatal diagnostic
output, not to the raised message itself — a captured `Failure.message`
carries the error, not usage advice.

### The error boundary: `call_result`

`call_result(fn, ...args)` invokes a callable and converts a runtime failure
that unwinds out of it into a value:

```noxy
use errors select *

let r = call_result(to_int, entrada)   // r: Result<int> — to_int returns int
if r.ok then
    print(r.value + 1)                  // r.value: int here (narrowed by r.ok)
else
    print("entrada inválida: " + r.failure.message)
end
```

**Signature.**

```noxy
call_result(fn, ...args)   // -> errors.Result<R>
```

`R` is the static return type of `fn`: a declared function or struct
constructor, a function value with an exact signature, a function literal,
or a core native with a known return type (`to_int` → `int`). A `void`
callee or one whose return type the compiler cannot see (a legacy native, a
bare `func` value) gives `Result<any>`. The `errors` module must be in scope
(`use errors select *`, or any `select *` that re-exports it, such as
`use convert select *`): `call_result` needs `Result` and `Failure` to build
its envelope, and a call without them is a compile error.

`fn` is the callable to invoke — always the first argument. `...args` are
zero or more arguments of any Noxy type (scalars, strings, bytes, arrays,
maps, struct instances, `null`, explicit `ref`s), forwarded to `fn` exactly
as a direct call would forward them.

**Accepted callables.** Every category of callable value the language has is
a valid `fn`. What varies is only *when* a misuse (wrong arity, wrong
argument type, wrong parameter mode) is reported:

| `fn` | Example | Misuse is reported… |
|---|---|---|
| Typed Noxy function | `call_result(dobro, 21)` | synchronously, in the caller |
| Closure / function literal | `call_result(func() -> int … end)` | synchronously, in the caller |
| Function value (exact or bare `func`) | `let f: func(int) -> int = dobro` then `call_result(f, 21)` | synchronously, in the caller |
| Struct constructor | `call_result(Ponto, 3, 4)` | synchronously (field count and field types) |
| Signed native | `call_result(to_int, entrada)` | synchronously, in the caller |
| Legacy unsigned native | — | during invocation; the failure is **captured** |

Passing a non-callable is a synchronous runtime error in the caller
(`call_result expects a callable, got <type>`). A misuse reported
synchronously raises in the caller like any other runtime error — it is
never wrapped in an envelope, because a wrong call site is a bug in the
program, not data. Only legacy natives without parameter metadata cannot be
pre-validated: their misuse surfaces inside the invocation and is captured,
indistinguishable from a failure of the callee's own body. (A native
reaches the boundary as `any` or in argument position; the compiler does not
admit `let f: func = to_int`.) The timing mirrors `spawn_task`'s synchronous
validation; the domain is wider — `spawn_task` accepts only Noxy functions
and closures, while `call_result` also accepts constructors and natives.
*Compatibility note:* giving a legacy native a signature in a later release
moves its misuse failures from captured to synchronous — an observable
change that rides the signing, and one more reason to sign natives eagerly.

For a struct constructor, a completed call yields the constructed instance as
`value`, under the constructor semantics the defer section already gives that
callee category.

```noxy
use errors select *

struct Ponto
    x: int
    y: int
end

func deposita(saldo: ref int, valor: int)
    *saldo = *saldo + valor
end

let c = call_result(Ponto, 3, 4)                   // constructor: c: Result<Ponto>
if c.ok then
    let p: Ponto = c.value                         // p.x == 3, p.y == 4
end

let saldo: int = 100
call_result(deposita, ref saldo, 30)               // explicit ref keeps identity: saldo == 130
```

**Arguments.** Arguments are evaluated in the caller's frame, before the
boundary exists; an error raised while *evaluating* an argument expression is
not captured. Passing follows §4.3 exactly as in a direct call: composite
values are independent copy-on-write values in the callee, and `ref`
arguments keep reference identity. Because `call_result` is a dynamic
boundary, a reference must be written explicitly as `ref value` (§4.2) —
`call_result(deposita, saldo, 30)` passes a plain copy and never manufactures
a reference. Where the callee exposes parameter metadata, the number of
`...args` must match its arity, and each argument must satisfy the declared
parameter type and mode — checked synchronously, per the table above.
`call_result` adds no isolation — it is the same call, wearing a boundary.

**Envelope.** `call_result` always returns an `errors.Result<R>`; it never
raises for anything that happens *inside* `fn`:

| Field | Type | `fn` completes | Failure captured |
|---|---|---|---|
| `ok` | `bool` | `true` | `false` |
| `value` | `R?` | `fn`'s return value; `null` for `void` | `null` |
| `failure` | `Failure` | `empty_failure()` (`kind == ""`) | the primary failure |

| `Failure` field | Type | Content |
|---|---|---|
| `kind` | `string` | `"runtime"` (Noxy runtime error) or `"panic"` (recovered Go panic) |
| `message` | `string` | the error message, clean of usage advice |
| `stack` | `string` | Noxy stack at the failure point for `"runtime"`; Go stack for `"panic"` |
| `causes` | `Failure[]` | deferred failures aggregated during the unwinding, LIFO; empty otherwise |

In detail:

- `fn` completes: `ok = true`, `value` is its return value (`null` for a
  `void` return), `failure = null`. A composite `value` preserves the identity
  an ordinary call would give it.
- A runtime failure unwinds out of `fn`: deferred calls registered by `fn` and
  its nested frames run under the normal unwinding rules, the unwinding stops
  at the `call_result` frame, and the envelope is `ok = false`,
  `value = null`, with `failure` describing the primary error.
- `failure.kind` is `"runtime"` for a Noxy runtime error (with a Noxy stack
  captured at the failure point) or `"panic"` for a recovered Go panic (with a
  Go stack) — the same vocabulary as the task boundary. A `"runtime"` stack
  covers the frames from the failure point down to, and excluding, the
  `call_result` frame itself.
- `failure.causes` holds deferred failures observed during that unwinding, in
  LIFO execution order, each a `Failure` whose own `causes` nest further, per
  the defer section's aggregation rules. It is empty when no deferred call
  failed. Each cause's `stack` is captured at that deferred failure point and
  carries the deferred call's registration location as its outermost frame —
  the envelope form of the defer section's promise that each deferred failure
  is collected "with its registration location".
- Cleanup as first failure: if `fn` returns successfully but one of its
  deferred calls raises, the boundary reports `ok = false` and the computed
  return value is discarded, mirroring "converts an otherwise successful
  return into a runtime error" in the defer section. The first deferred
  failure is the primary `failure`; deferred failures observed after it
  aggregate under the primary's `causes` in LIFO order, each nesting its own
  `causes` further.

**Representation.** The envelope is a genuine instance of the monomorphized
struct `errors::Result<R>` — the compiler instantiates it at the call site
(the same queue every generic use goes through) and hands the native the
constructors of that instance and of `Failure` as hidden leading arguments.
`fmt("%T", r)` reports the instance name, `r == Result(true, 5,
empty_failure())` compares equal by content, the envelope passes nominal
checks (typed task arguments, typed slots) like any struct value, and the
`Failure` tree — including every entry of `causes` — is made of `Failure`
instances. Each compilation unit instantiates its own `Result<R>`; values
flow between them by name, so a `Result<int>` built inside `convert` is a
`Result<int>` in the program that called `to_int_result`. The
structurally-matching-map admission at dynamic-boundary annotations (§4.2)
still exists for natives that return maps, but no stdlib envelope relies on
it any more; `task_await`'s envelope remains a map (follow-up).

**What the boundary does not change.**

- The caller's own deferred calls and frames are unaffected; after
  `call_result` returns, execution continues normally.
- Boundaries nest: a failure is captured by the nearest enclosing
  `call_result` frame.
- Detached routines started by `fn` via `spawn` keep running after capture,
  per the `spawn` contract.
- `call_result` is an ordinary call and is legal anywhere a call is,
  including module initialization; frame rules follow the defer section
  unchanged.
- **Through native frames.** A failure raised in Noxy code that a native
  invoked (a callback, an HTTP handler) propagates through the intervening
  native frame to the nearest boundary; the native observes the failure
  through its existing error path and releases its resources before
  propagation continues. A callback-invoking native must forward Noxy
  failures, never swallow them.
- **No rollback.** Mutations `fn` performed before failing — through `ref`
  arguments, globals, closure upvalues, or native resources — remain. Noxy's
  copy-on-write value semantics can suggest that everything is isolated; the
  boundary is control flow, not a transaction. The risky shape has a name:
  a callable that mutates `ref` arguments, globals, or upvalues can leave a
  broken invariant behind when captured mid-flight — prefer value-in/value-out
  callables under a boundary, and treat mutating ones as candidates for
  explicit coordination, exactly as across the task boundary.
- **Panic coverage.** Go panics are recovered in the executing routine only,
  not in independent goroutines started by native code — the same coverage
  rule as supervised tasks. Exhaustion of the VM's Noxy frame limit is an
  ordinary runtime error and is capturable (`kind = "runtime"`): by the time
  the boundary observes it, unwinding has already freed frames. Conditions
  the Go runtime treats as unrecoverable (fatal errors such as concurrent map
  writes, Go stack exhaustion, out of memory) remain process-fatal; no
  boundary observes them. The recover covers the invocation of `fn` itself;
  a Go panic raised outside it (argument preparation, envelope construction)
  remains process-fatal like any other unrecovered panic.

**Intended idiom.** `call_result` exists to build named `_result` twins, not
to decorate call sites. The inline form over a closure is legal:

```noxy
let r = call_result(func() -> int
    return to_int(campo) * fator
end)                                   // r: Result<int>
```

but the idiom is wrap-and-name: a `_result` function returning `Result<T>`
is a contract; an inline boundary is a site the reader must decode. The
boundary's design leans on auditability — `call_result` is one grep away
from every place errors become data. Discarding the envelope —
`call_result(f, x)` as a bare statement — swallows every failure `f` can
produce and is almost certainly a bug; a compile-time diagnostic for it is
tracked as follow-up.

### Propagation: `try`

`try expr` is the one piece of sugar over `Result<T>`. `expr` must have type
`Result<U>`, and the enclosing function must return `Result<V>`:

- if `expr.ok`, the expression's value is `expr.value`, of type `U`;
- otherwise the function returns **immediately** with
  `Result<V>(false, null, expr.failure)` — the same `Failure`, untouched.
  Deferred calls run as for any `return`.

```noxy
use errors select *
use convert select *

func le_config(texto: string) -> Result<Config>
    let bruto: string = try le_texto(texto)     // Result<string> → string, or return the failure
    let porta: int = try to_int_result(bruto)   // Result<int> → int, or return the failure
    return Ok(Config(porta))
end

try io.close_result(f)                          // Result<bool> as a statement: only propagates
```

`try` binds like a prefix operator (`try f(x).ok` is `try (f(x).ok)`), is a
reserved word, and is a compile error outside a function (`'try' outside a
function`), in a function whose return type is not a `Result`
(`'try' requires the enclosing function to return Result<T> (found void)`),
or on an operand that is not a `Result` (`'try' expects a Result<T>, got
int`). `Result` is recognized by name — the struct declared in `errors` —
not by shape, so a user struct that happens to have an `ok` field does not
get `try`.

### When / Channel Select

`when` evaluates a set of channel operations and runs the body of the first
`case` that is ready, exactly once:

```noxy
let ch1: any = make_chan(1)
let ch2: any = make_chan(1)

chan_send(ch1, "hello")
when
    case msg = chan_recv(ch1) then
        print("Received from ch1:", msg)
    case chan_recv(ch2) then
        print("Received from ch2")
    default
        print("Default case (should not happen if ch1 ready)")
end
```

Each `case` is one of:

- `case <var> = chan_recv(<chan>) then` — receives and binds the value to a
  new variable, scoped to that case's body.
- `case chan_recv(<chan>) then` — receives and discards the value, useful
  when only the channel's readiness matters.
- `case chan_send(<chan>, <value>) then` — succeeds when the send can
  complete immediately.

An optional `default` case runs immediately when no other case is ready,
making the `when` non-blocking. Without `default`, `when` blocks until one
case becomes ready:

```noxy
let ch3: any = make_chan()
let ch4: any = make_chan()

spawn(sender, ch3, "delayed_msg_3", 100)
spawn(sender, ch4, "delayed_msg_4", 50)

when
    case m1 = chan_recv(ch3) then
        print("Got from ch3:", m1)
    case m2 = chan_recv(ch4) then
        print("Got from ch4:", m2)
end
```

Any value a channel can carry — including a generic struct instance such as
`Caixa<int>` (§6) — travels through `chan_send`/`chan_recv` and is received
by `when`/`case` exactly like any other value.

### Limites de chamada

A profundidade de chamada é limitada apenas pela memória: cada VM começa com
64 frames e 4096 slots de operandos e **cresce sob demanda** até os tetos de
**100 000 frames** e **1 048 576 slots** (§13). Uma recursão de 50 000 níveis
roda; uma recursão infinita para com erro de runtime, nunca com um panic:

```text
Runtime error: [programa.nx:line 2] stack overflow: call depth exceeds 100000 frames
```

Uma única expressão que empilhe temporários demais (um literal gigantesco)
esbarra no outro teto:

```text
Runtime error: [programa.nx:line 7] stack overflow: operand stack exceeds 1048576 slots
```

Os dois são erros de runtime comuns: dentro de `call_result` viram um
`Failure` capturável, como qualquer outro erro.

O stack Noxy capturado em erros de runtime (`Failure.stack`) mostra no máximo
96 frames — os 64 mais internos e os 32 mais externos, com uma linha
`... N frames omitted ...` no meio — para não reter uma string gigante em
`call_result`/`task_await` numa recursão profunda.

---

## 8. Expressions

### Mathematical
`+`, `-`, `*`, `/`, `%`

`int` arithmetic **wraps around** on overflow (two's complement, exactly like
Go): `+`, `-` and `*` never raise an overflow error.

```noxy
print(9223372036854775807 + 1)   // -9223372036854775808
```

Division by zero and `%` by zero are runtime errors; overflow is not. When a
range matters, check it before the operation (or work in `float`).

`%` is defined for `int` only; the float remainder, roots, powers, rounding
and trigonometry live in the `math` module (§12): `math.fmod(x, y)`,
`math.sqrt(x)`, `math.floor(x)`, `math.atan2(y, x)`.

### Comparison
`>`, `<`, `>=`, `<=`, `==`, `!=`

Ordering (`>`, `<`, `>=`, `<=`) is defined for **numbers** (with the usual
int/float promotion) and for **strings**. String ordering is lexicographic
and byte-exact — for valid UTF-8, which every Noxy string is by invariant,
this is identical to code-point order, mirroring Python. Ordering any other
operand pair, including `bytes`, is an error (`operands must be numbers or
strings` — at compile time when both static types are known, see *Static
operand checks* below; at runtime otherwise); bridge bytes through `to_str`
first. Equality
(`==`, `!=`) is structural for every type (§2.2, rule 7).

### Logical
- `&&` (AND)
- `||` (OR)
- `!` (NOT)

All three require `bool` operands — there is no truthy/falsy conversion (§7).
With a known static type that is not `bool` the program is rejected at compile
time (`operand of '!' must be bool, got int`, `logical operators require
boolean operands, got int and bool`); an `any` operand is checked at runtime.
A `ref bool` operand is never read implicitly (§2.3 R2), in `&&`/`||` or in a
condition: `r || x` is a compile-time error (hint: `use '*r'`), and so is
`if r then` — write `*r || x` and `if *r then`.

### Bitwise
- `&` (AND)
- `|` (OR)
- `^` (XOR)
- `~` (NOT)
- `<<`, `>>` (Shift)

The bitwise operators are strictly bitwise: they are never a substitute for
`&&`/`||`. `&`, `|` and `^` accept two `int` or two `bytes` of the same length;
`<<` and `>>` accept `int` only; `~` accepts `int` or `bytes` (every byte is
inverted). Wrong static types are compile-time errors with the same text as
the runtime check (`[line N] operands for & must be integers or bytes, got int
and bool`, `operand of '~' must be int or bytes, got bool`).

### Static operand checks

The arithmetic and ordering operators are checked the same way as the bitwise
ones. When both operands have a known static type (anything but `any` or an untyped dynamic
value), the compiler applies the runtime rules up front and rejects a
mismatch with the runtime message plus the two types:

| Operator | Accepts | Compile-time error text |
|----------|---------|-------------------------|
| `+` | `int`/`float` (mixed promotes), `string + string`, `bytes + bytes` | `operands must be numbers or strings or bytes, got int and string` |
| `-`, `*`, `/` | `int`/`float` | `operands must be numbers, got bool and int` |
| `%` | `int % int` | `operands for % must be integers, got float and int` |
| `<`, `>`, `<=`, `>=` | `int`/`float`, or `string` with `string` | `operands must be numbers or strings, got bytes and bytes` |

`==`/`!=` keep their own rules (§2.3). A `ref T` operand is an error — read it
with `*r` (§2.3 R2). `any` and untyped values stay on the generic opcode and are
checked at runtime, so the dynamic boundary is unchanged; the bytecode of a
valid program is identical with or without the check. Inside a generic
function the check runs per instance, so `soma(true, false)` on
`func soma<T>(a: T, b: T)` reports `em soma<bool> (instanciado na linha N):
operands must be numbers or strings or bytes, got bool and bool`. Like every
static check, it also rejects a mismatch in code that would never run
(`if false then print(1 + "a") end`).

```noxy
let x: int = 10
print(x + "ola")    // ERROR: [line 2] operands must be numbers or strings or bytes, got int and string
let v: any = 10
print(v + "ola")    // compiles; fails at runtime with the same message
```

Unary `*` is the dereference operator and applies only to a `ref`. With a known
non-`ref` static type it is a compile-time error, so `2 ** 3` (there is no
exponentiation operator in Noxy) reports
`[line N] cannot dereference non-reference value of type int`.

---

## 9. F-Strings

String interpolation with `f"..."`.

```noxy
let name: string = "Noxy"
print(f"Hello, {name}!")
```

Each `{e}` is compiled as `to_str(e)` and the pieces are joined with `+`.
`to_str` is a core builtin, so an interpolation never ends a narrowing
(§2.4): `if m != null then print(f"{m['a']} {m['b']}") end` compiles with `m`
a global `map[string, any]?`.

**Literal braces.** `{{` produces `{` and `}}` produces `}`:

```noxy
let x: int = 1
print(f"{{x}} = {x}")      // {x} = 1
print(f"{{{x}}}")          // {1}
```

**No format specs.** Anything left over after the interpolated expression is a
syntax error — there is no `:spec` mini-language, so `f"{name:>10}"` reports:

```text
[2:7] SyntaxError: unexpected ":" in f-string expression
  hint: format specs are not supported; use fmt("%10s", x) for width/precision
```

Use `fmt` for width and precision: `fmt("%6.2f", value)`, or interpolate the
call itself: `f"{fmt("%10s", x)}"`.

**Quotes inside `{}`.** An interpolated expression may contain string
literals delimited by the same quote as the f-string (the Python 3.12 rule,
PEP 701): the lexer tracks brace depth, so the inner quote opens a nested
literal instead of closing the f-string.

```noxy
let n: int = 7
print(f"n = {fmt("%03d", n)}")   // n = 007
print(f"{"a"}")                   // a
```

A `{` that is still open at the end of the line is reported where it starts:
`SyntaxError: unclosed brace in f-string` with the hint `every '{' that starts
an expression needs a matching '}'; write '{{' for a literal brace`.

**An expression that starts with `{`** (a map literal) needs a space, otherwise
the `{{` is read as an escape — the same rule as Python:

```noxy
print(f'{ {"a": 1}["a"] }')   // 1
```

## 10. Built-in Functions

### I/O
- `print(args...)`: prints to **stdout**, followed by a newline.
- `iprint(args...)`: the same, **without** the trailing newline.
- `eprint(args...)`: prints to **stderr**, followed by a newline.
- `eiprint(args...)`: stderr, without the trailing newline.
- `input(prompt?) -> string`: reads one line from stdin.

Program output goes to stdout; diagnostics go to stderr. Every message the
VM/CLI itself produces — parser and compiler errors, `Runtime error:` and its
`hint:`, `Error reading file:`, thread and plugin errors — is written to
**stderr**, so a shell pipeline that used to capture them from stdout now needs
`2>&1`. `eprint`/`eiprint` are the Noxy-level equivalent of
`fprintf(stderr, ...)`.

```noxy
eprint("could not open:", path)   // arguments are joined with a space
```

`input(prompt)` prints the prompt (always, including at end of input), reads a
line from stdin and returns it without the trailing `\n`/`\r\n`. All calls
share one reader, so it also works with redirected stdin (`noxy p.nx < in.txt`)
— reading every line, not just the first. **`input()` does not signal EOF**: at
end of input it returns `""`, indistinguishable from an empty line. To tell
them apart, read stdin as a file: `io.read_line(io.stdin())` returns
`ok=false, error="EOF"` (§12).

### Conversions
- `to_str(val)`
- `to_int(val)`
- `to_float(val)`
- `to_bytes(val)`

### Conversões numéricas

A regra geral levantar-vs-`_result` está em *Errors: raise for bugs, results
for data*.

```noxy
to_int(5.9)      // 5, truncamento em direção a zero
to_int("5")      // 5
to_int("5.5")    // erro: uma string decimal não é um inteiro
to_int("abc")    // erro
to_int(true)     // erro: bool não é número em Noxy
```

```noxy
use convert select *

let porta = to_int_result(getenv("PORT").value)   // Result<int>
if porta.ok then
    print("porta " + to_str(porta.value))
else
    print("PORT inválida: " + porta.failure.message)
end
```

Validar antes de converter não é uma alternativa correta: `is_digit` aceita
`"9999999999999999999"`, que estoura `int64`, e não há como checar o intervalo
sem converter. Não existe `is_float`.

The core builtins have static return types where the result never varies —
`length`, `to_str`, `to_int`, `to_float`, `to_bytes`, `type`, `input`, `fmt`,
`hex`, `hex_encode`, `hex_decode`, `ord`, `contains`, `has_key`, `json_dumps`,
`keys(map[K, V]) -> K[]`, `slice` (same type as its first argument) — so a
value of theirs is checked like any other expression (`let s: string =
length(xs)` is a compile error) and can initialize an inferred `let` (§3).
They — together with `print` and `range` — are pure natives that never run
Noxy code, so a call to one of them never ends a narrowing (§2.4).
`call_result` is typed by its callee (`errors.Result<R>`, §7). The others
(`json_parse`, `task_await`, `make_chan`, ...) are dynamic-boundary builtins:
their result is `any` or untyped and needs an annotation. A function the
program declares with a builtin's name shadows the builtin, static type
included.

### Collections
- `length(arr_or_map) -> int`
- `append(ref arr, val)`
- `pop(ref arr) -> T`, `pop(ref arr, i) -> T`: removes and returns the last
  element, or the element at index `i` (the rest shifts down, order
  preserved, O(n)) — Python's `list.pop([i])`. A position that does not
  exist is a **runtime error**: `pop from empty array` for `pop(ref arr)` on
  an empty array, `array index out of bounds` for an index outside
  `[0, length)`. Test `length(arr) > 0` first; `pop` never returns `null`.
- `swap_remove(ref arr, i) -> T`: removes and returns the element at `i` by
  moving the **last** element into its place (O(1), order not preserved) —
  what a game loop wants when order does not matter. Same range rule.
  Both go through the same copy-on-write path as `append`: a copy taken
  before the call does not see the removal. `delete` remains map-only.
- `keys(map)`: Returns array of keys.
- `has_key(map, key)`: Returns bool.
- `delete(ref map, key)`
- `range(stop)`, `range(start, stop)`, `range(start, stop, step) -> int[]`:
  the Python sequence — `stop` is exclusive, a negative `step` counts down,
  an empty interval is `[]` (`range(5)` → `[0, 1, 2, 3, 4]`,
  `range(10, 0, -3)` → `[10, 7, 4, 1]`, `range(3, 0)` → `[]`). It is a
  runtime builtin: no import. Arity (1 to 3) and argument types (`int`) are
  checked at compile time and the result is typed `int[]`, so
  `for i in range(n)` gives `i: int`. `step == 0` is a runtime error. The
  array is materialized, so a sequence of more than 2³¹−1 elements is a
  runtime error too (`range: sequence too large`), not an out-of-memory
  crash. A `func range` declared by the program shadows the builtin, like
  any other builtin name.

### Utils
- `addr(ref var)`: Returns the memory address/identity of a variable as a string.
- `zeros(n)`: create zeroed array.
- `hex_encode(data: bytes) -> string`: Converts bytes to hexadecimal string.
- `hex_decode(hex: string) -> bytes`: Converts hexadecimal string to bytes.
- `fmt(format, args...)`: printf-style formatting.
  - `%s`: String
  - `%d`: Integer (Base 10)
  - `%x`, `%X`: Integer (Hex)
  - `%b`: Integer (Binary)
  - `%f`: Float
  - `%.Nf`: Float with N decimal places
  - `%e`: Float (Scientific notation)
  - `%v`: Any value (Default representation)
  - `%t`: Boolean
  - `%q`: Quoted string/bytes
  - `%T`: Runtime type name of the value (same table as `type(v)` below)

```noxy
let msg: string = fmt("Value: %d, Hex: %x", 255, 255)
// "Value: 255, Hex: ff"

// Hex encoding example
let data: bytes = b"Hello"
let hex: string = hex_encode(data)  // "48656c6c6f"
let back: bytes = hex_decode(hex)   // b"Hello"
```

### Type inspection

`type(v: any) -> string` returns the runtime type name of a value. Its main
use is inspecting `any` values at dynamic boundaries (`task_await`
envelopes, JSON, channel payloads). The names:

| Value | `type(v)` |
|-------|-----------|
| `int`, `float`, `bool`, `string`, `bytes` | `"int"`, `"float"`, `"bool"`, `"string"`, `"bytes"` |
| `null` | `"null"` |
| array | `"array"` |
| map | `"map"` |
| struct instance | the nominal name — `"Pessoa"`, `"Caixa<int>"` (no module qualifier) |
| struct definition (the constructor as a value) | `"struct"` |
| function, closure, or native | `"function"` |
| `ref` | `"ref"` |
| task handle | `"task"` |
| channel / waitgroup | `"channel"` / `"waitgroup"` |

```noxy
struct Caixa<T>
    valor: T
end

print(type(1))          // int
print(type(Caixa(1)))   // Caixa<int>
print(type(null))       // null
```

`type` reports the runtime representation, not the static annotation: a
`call_result` envelope reports `"Result<int>"` (it is a genuine
`errors.Result<int>` instance — the struct-instance row above — never
`"map"`), and any value bound to `any` reports what it actually is. The
`%T` verb of `fmt` uses the same table.

### Concurrency and Supervised Tasks

`spawn(function, ...arguments)` starts a detached Noxy routine and immediately returns `null`. It exposes no handle and does not propagate the worker's result, runtime error, or panic to its caller. Since 0.4.0 its arguments follow the normal value semantics (the legacy identity-forwarding exception was removed): composite arguments are independent values in the worker. Its existing validation and asynchronous diagnostics remain compatible.

`spawn_task(function, ...arguments)` instead validates a Noxy function or closure, arity, and parameter modes synchronously, launches it in a shared child VM, and returns an opaque task handle. Handles may be stored as `any`, passed, printed, and compared by identity, but cannot be constructed or inspected by Noxy code.

`task_await(handle)` waits indefinitely. `task_await(handle, timeout_ms)` accepts a non-negative integer number of milliseconds; zero performs an immediate poll. Invalid handles or timeouts are synchronous runtime errors in the caller.

Each successful await returns a fresh `map[string, any]` envelope:

| Status | `value` | `error` |
|---|---|---|
| `"ok"` | The task's return value, including `null` for a void return | `null` |
| `"error"` | `null` | A structured failure map |
| `"timeout"` | `null` | `null` |

For `"error"`, the failure map contains string fields `kind`, `message`, and `stack`. `kind` is `"runtime"` for a Noxy runtime error or `"panic"` for a recovered Go panic. Runtime stacks are Noxy stacks captured at the failure point; panic stacks are Go stacks. The panic boundary covers the supervised task's main goroutine only, not independent goroutines started by native code.

Task completion is published exactly once and can be awaited consistently by multiple sequential or concurrent waiters. Each envelope and failure map is a fresh container, but an `"ok"` composite value preserves its original identity. Consequently, replay means the same terminal outcome and returned-value identity, not independent deep snapshots.

Timeout is local and non-terminal: it does not cancel the worker, consume the result, or mutate the task. Completion is preferred whenever it is observably available at the deadline. A wait that returns `"timeout"` may therefore be followed by a later wait that returns `"ok"` or `"error"`.

Supervised tasks share globals, module state, runtime resources, closure environments, and the VM configuration. Ordinary composite arguments follow value semantics (independent through value fields, copy-on-write; a ref-carrying argument shares its `ref` targets — §2.2 rule 6) and `ref` arguments retain reference identity. Intentionally shared state — globals, `ref` (as argument or as field), closure upvalues — still requires explicit concurrency coordination.

---

## 11. Module System

### Basic Import
```noxy
use strings
print(strings.to_upper("hello"))
```

### Alias
```noxy
use strings as s
print(s.to_lower("HELLO"))
```

### Selective Import
```noxy
use strings select to_upper, to_lower
print(to_upper("hello"))
```

`select` binds functions and structs **by name**. For a top-level `let`
variable it binds a **snapshot**: the value is copied at import time, and later
updates made by the module are not visible through the imported name. Use the
namespace form (`m.x`) to observe live module state.

```noxy
use counter select total    // snapshot of counter.total at import time
use counter                 // counter.total reads the live value
```

### Module state is writable through the namespace

Assigning to a module variable through the namespace is legal and typed
with the member's declared type, and the write lands in the module's live
state — reads through the namespace were already live; writes are too.
`select` still binds a snapshot.

```noxy
use counter
counter.total = 9          // OK: total is `let total: int` in counter.nx
print(counter.read())      // 9 — the module sees the new value
counter.total = "a"        // ERROR: type mismatch in assignment to
                           // 'counter.total': expected int, got string
counter.nope = 1           // ERROR: 'counter' has no member 'nope'
counter.read = 1           // ERROR: cannot assign to 'counter.read': it is a function
```

Nested writes (`m.origin.x = 99`, `m.xs[i] = v`), `ref m.x`, `append(ref
m.xs, v)` and `pop(ref m.xs)` follow the same rule with the member's type.
A `ref T` member accepts only a rebind (`m.link = ref other`), like a `ref`
global. A global `let` that shadows the namespace name is a different
binding and is unaffected. As with any shared global, concurrent writers
must coordinate (docs/concurrency.md): a single write is synchronized, a
read-modify-write is not.

A write through a namespace reaches the binding of **that alias's module**,
even when the member's *type* resolves through the module that declared it:
a member that only reaches `mid` by re-export (`mid.nx` does `use base
select *`) is `mid`'s own snapshot, so `mid.x = 5` makes `mid.x` read 5 while
`base.read_x()` still returns 1. Write through the declaring module's own
namespace (`use base` and `base.x = 5`) to change the live state.

The namespace object is a **view of the module's live state**, not a value
with copy semantics: binding it to another name (`let s: any = m`) or passing
it to a `func(x: any)` never detaches it, so a write through that name is
still seen inside the module (in Python, Go and Nim a module is likewise a
reference).

A member whose declared type the program cannot write — an instance of a
generic struct of the module (§1.6) — is **read** dynamically but cannot be
**assigned**: `cannot assign to 'g.c': its type cannot be translated here (it
involves an instance of a generic struct of 'g')`, with a hint to expose a
function in the module that updates it. An unchecked write there would store another type
in the module's own global and break the module from the inside.

### Struct identity across import forms

A struct imported by namespace (`geometry.Point`) and the same struct imported
by `select` (`Point`) are the **same nominal type** — a value of one is
accepted wherever the other is expected, including inside function types
(`func(Point)` ≡ `func(geometry.Point)`), when inferring a generic parameter,
and as the type of a **struct field**: `file: io.File` and `file: File` (after
`use io select File`) declare the same field, and the enclosing struct's
constructor accepts the same values. A field typed with an imported struct is
resolved in the scope of the module that declared it, so `a: geometry.Segment`
works even when `Segment` has a field of another `geometry` struct the program
never imported. A locally declared `Point` is a different type and is never
compatible with `geometry.Point`.

A qualified field type that does not resolve is a compile error at the struct,
never a runtime failure of its constructor:

```text
[line 3] struct 'Reader' field 'file': cannot resolve type 'io.Nope': module 'io' has no struct 'Nope'
  hint: check the struct name against the structs declared in 'io'
```

(`'foo' is not an imported module` + `hint: add 'use foo' at the top of the
file` when the namespace itself is unknown.) The same diagnostics apply to a
qualified name in a parameter, return type or `let` annotation.

### Member access on values of module struct types

Reading, assigning or taking `ref` of a field of a value whose type is a
module struct (`a.f.path` with `f: io.File`; `res.rows` with
`res: sqlite.QueryResult`, or `QueryResult` after `use sqlite select
QueryResult`) is **statically typed**, exactly like a field of a local struct.
The field's declared type is written in the module's own vocabulary
(`rows: Row[]` inside `sqlite.nx`), so the compiler **translates it to the
program's view**, name by name, before using it:

| The program imported `Row` as… | `res.rows` has type |
|---|---|
| `use sqlite select Row` (or `select *`) | `Row[]` — the name the program wrote |
| `use sqlite` / `use sqlite as db` (namespace only) | `sqlite.Row[]` / `db.Row[]` — the first alias declared, when there are several |
| neither (the program cannot write `Row`) | `sqlite.Row[]` — the canonical path (module + name); the value is fully typed, the name is display only |

The translation applies to nested types (`Row[]`, `map[string, Row]`,
`ref Row`, `func(Row) -> Row`) and along a chain (`w.o.i.v` re-applies it at
each step).

**A value's type is its declaration; a name is needed only to write an
annotation.** Two names that designate the same declaration are the same
type (`Row`, `sqlite.Row`, `db.Row`, and the canonical `sqlite.Row` printed
for a program that imported none of them); a local `struct Row` is a
different type from any of them. Messages print an alias the program can
see: an alias counts when its `use` line precedes the line that imports the
value's binding (a `select`) or the point where the type is inferred,
skipping an alias shadowed by a local or parameter there; otherwise they
print the canonical path (`sqlite.Row`). Write `use m` before `use m select
…` if you want `m.V` in messages — with `mid.nx` re-exporting `V`: `use mid
select mkv` then `use mid` prints `base.V`; swapped, it prints `mid.V`.

A field whose type is an instance of a generic struct of
the module itself (`c: Caixa<int>` declared inside the module) stays
dynamic: the instance's internal name is not an identity across compilation
units. `null` is accepted wherever such a (qualified or translated) struct
type is expected, as for a local struct. In the REPL, `use` aliases and the
imported structs' origins carry over to later lines, so `use io` on one line
and `let f: io.File = ...` on the next behave as in a file. Because the names are the program's own, both annotations
then type-check: `let r: sqlite.Row = res.rows[0]` and, with the `select`,
`let r: Row = res.rows[0]`. Writing something wrong about such a field is a
compile error — `let x: string = a.f.fd` (`expected string, got int`),
`a.f.fd = "x"`, `inc(ref a.f.path)` against `inc(r: ref int)` — instead of a
runtime failure.

### Member access through a namespace

`m.f(...)`, `m.x` and `m.T(...)` after `use m` (or `use m as alias`) are
**statically typed** with the declaration the module exports — the same
signature `use m select f` binds — translated to the program's view by the
table above (`V` inside `vec.nx` reads as `vec.V`, or `V` after `select V`).
Consequently a namespace call is checked like any typed call: arity,
argument types (`argument 1 to 'm.roll': expected int, got string`) and
result type (`let s: string = m.roll(6)` is `expected string, got int`), and
`let v = m.roll(6)` infers `v: int`. A `T?` result is checked too: `let t:
bytes = crypto.aes256_gcm_decrypt(k, d)` is `expected bytes, got bytes?`. A
member whose type is an instance of a module's own generic struct stays
dynamic (§6.4); a struct the program merely cannot name is typed as above.
A generic template is still not reachable through the namespace — import it
with `select`. The module object itself has no type: `m` alone is not a
value.

A member that only reaches `m` by re-export (`m` does `use x select *` and
never declares it) resolves to its declaration in `x`, through `m.g` and
through `use m select g` alike.

### Unknown type names

Every type name in a `let` annotation, a parameter, a return type or a struct
field must name a known type: a primitive, a struct declared in the program
(anywhere in the file for top-level structs — forward references and self
references resolve; a struct declared inside a function is visible in that
body after its declaration), a struct imported by `select`/`select *`, a
qualified `m.T` through a namespace import, or a generic instance
(`Caixa<int>`). A function's signature is resolved in the scope where the
function is declared, so a struct that only exists inside its body cannot be
a parameter or return type. Anything else is a compile error:

```text
[line 2] struct 'A' field 'b': unknown type 'Inexistente'
  hint: declare 'struct Inexistente' or import it with 'use m select Inexistente'
```

When a loaded dependency declares that name, the hint says where to get it:
`add 'use base' or 'use mid select V' to name this type`.

The position reads `struct 'A' field 'b'`, `function 'f' parameter 'x'`,
`function 'f' return type` or `variable 'x'`. A module struct is only nameable
if the program imported it: with only `use io`, `let f: File = io.open(...)`
is an error — write `io.File` or add `use io select File`. A top-level `use …
select` counts for the whole file, so a signature declared before the `use`
line already sees the struct. An instance of an imported generic struct whose
field names another struct of its module needs that struct imported as well
(`use m select Caixa, Meta`, §6.4).

---

## 12. Standard Library

Noxy comes with a comprehensive standard library. Available modules include:

| Module | Description |
|--------|-------------|
| `io` | Input/Output operations (read/write files) |
| `strings` | String manipulation (upper, lower, replace, split) |
| `math` | Floating-point math: roots, powers, rounding, trigonometry, `min`/`max`/`clamp`, `PI`/`E` |
| `time` | Time and Date functions |
| `sys` | System interactions (argv, exit, env) |
| `net` | Network sockets (TCP/UDP) |
| `http` | HTTP Client and Server |
| `json` | JSON parsing and stringification |
| `crypto` | Cryptographic functions (hashing, UUID) |
| `sqlite` | SQLite database support |
| `rand` | Random number generation |
| `errors` | `Failure`, `Result<T>`, `Ok`/`Err`/`Fail`/`empty_failure` — the error-as-data vocabulary (§7) |

### Math (`math`)

Thin wrappers over the host's `math` library, all on `float`; angles are in
radians. `PI` and `E` are module bindings (`math.PI`, or `use math select
PI`).

| Function | Contract |
|----------|----------|
| `sqrt(x)`, `cbrt(x)` | square and cube root; `sqrt` needs `x >= 0` |
| `pow(x, y)` | `x` to the power `y`; errors for `x == 0 && y < 0` and for `x < 0` with a non-integer `y` |
| `abs(x)` | absolute value |
| `floor(x)`, `ceil(x)`, `round(x)`, `trunc(x)` | rounding, as `float`; `round` sends halves **away from zero** (`round(2.5) = 3.0`, `round(-2.5) = -3.0`), like Go — use `to_int` for an `int` |
| `fmod(x, y)` | remainder with the sign of `x` (`fmod(-7.0, 3.0) = -1.0`); `y == 0` is an error |
| `sin(x)`, `cos(x)`, `tan(x)`, `asin(x)`, `acos(x)`, `atan(x)` | trigonometry; `asin`/`acos` need `-1 <= x <= 1` |
| `atan2(y, x)` | angle of the vector `(x, y)` in `(-PI, PI]` — note the argument order, as in C |
| `hypot(x, y)` | `sqrt(x*x + y*y)` without intermediate overflow |
| `exp(x)`, `log(x)`, `log2(x)`, `log10(x)` | exponential and logarithms; `log*` need `x > 0` |
| `min(a, b)`, `max(a, b)`, `clamp(x, lo, hi)` | on `float`; `clamp` needs `lo <= hi` |
| `abs_int(x)`, `min_int(a, b)`, `max_int(a, b)`, `clamp_int(x, lo, hi)` | the same on `int` (no overloading in the language, hence the suffix) |

**Domain errors raise.** An argument outside the function's domain is a
runtime error, never `NaN` — the same rule as `1.0 / 0.0` (§8) and as
Python's `math`:

```text
Runtime error: [math:line 12] native 'math_sqrt' failed: math.sqrt: domain error (x < 0), got x=-1
```

Overflow is not checked: `exp(1000.0)` and `pow(10.0, 400.0)` return `+Inf`,
as `int` arithmetic wraps (§8). Arguments must be `float`: `sqrt(4)` is a
compile-time error (`expected float, got int`); write `sqrt(4.0)` or
`sqrt(to_float(n))`.

```noxy
use math
let ang: float = math.atan2(dir.y, dir.x)     // radians → degrees: * 180.0 / math.PI
let d: float = math.hypot(dx, dy)
let speed: float = math.clamp(v, 0.0, MAX)
```

### I/O (`io`)

Every fallible operation reports through a result struct instead of raising
(§7, *Errors: raise for bugs, results for data*):

| Struct | Fields |
|--------|--------|
| `File` | `fd: int`, `path: string`, `mode: string`, `open: bool` |
| `IOResult` | `ok: bool`, `data: string`, `error: string` |
| `IOBytesResult` | `ok: bool`, `data: bytes`, `error: string` |
| `IOLinesResult` | `ok: bool`, `data: string[]`, `error: string` |
| `IOWriteResult`, `IOCloseResult` | Raw shapes of the underlying natives; the public wrappers below return `errors.Result<T>` |
| `IOPositionResult` | `ok: bool`, `position: int` (absolute byte offset; `-1` when `ok=false`), `error: string` |
| `FileInfo` | `exists: bool`, `size: int`, `is_dir: bool` |

Every open `File` has one **cursor** — a byte position that every read and
write starts from and advances. A freshly opened file starts at 0 (`"a"`
writes always go to the end, as the OS defines append mode). `seek`/`tell` move
and query it; the constants `SEEK_SET = 0`, `SEEK_CUR = 1`, `SEEK_END = 2` name
the three origins, as `lseek` does in C.

| Function | Contract |
|----------|----------|
| `open(path, mode) -> File` | `mode` is `"r"`, `"w"` (truncate), `"a"` (append) or `"rw"`/`"r+"` (read and write in place, no truncation). On failure the `File` comes back with `open=false` |
| `stdin() -> File` | The process's standard input as a `File` (`path="<stdin>"`, read-only, not closable, not seekable). Always the same handle |
| `close(file) -> void` | Closes and forgets the handle |
| `close_result(file) -> Result<bool>` | Same, reporting the outcome (`ok=false`, `failure.message="stdin cannot be closed"` for `stdin()`) |
| `read(file) -> IOResult` | Everything **from the cursor to the end** as text (the whole file on a fresh handle; what is left after `read_line`/`read_n`/`seek`/`write`; `""` with `ok=true` when already at the end). Leaves the cursor at the end |
| `read_lines(file) -> IOLinesResult` | Same range split by line, `\r\n` normalized, **with no trailing `""`**: `"a\nb\n"` and `"a\nb"` both give `[a, b]`; `""` gives `[]` |
| `read_bytes(file) -> IOBytesResult` | Same range as raw `bytes`, no UTF-8 validation |
| `read_line(file) -> IOResult` | **Incremental**: the next line from the cursor, without `\r\n`. At end of file `ok=false, data="", error="EOF"`; a last line with no `\n` is returned normally and the next call reports EOF |
| `read_n(file, n) -> IOBytesResult` | **Incremental**: up to `n` raw bytes from the cursor. Fewer than `n` only at the end of the file; nothing left gives `ok=false, error="EOF"`; `n = 0` gives `b""` with `ok=true`; `n < 0` is an error. Works on `stdin()` |
| `seek(file, offset, whence) -> IOPositionResult` | Moves the cursor to `offset` bytes from the start (`SEEK_SET`), from the current position (`SEEK_CUR`) or from the end (`SEEK_END`) and reports the new absolute position. Past the end is allowed (a later read reports EOF; a write extends the file). Errors: `"stdin is not seekable"`, `"invalid whence N (...)"`, a negative resulting position (cursor unchanged), `"File not open"` |
| `tell(file) -> IOPositionResult` | The current cursor position (`"stdin is not seekable"`, `"File not open"`) |
| `write(file, content: string) -> void` | Writes text **at the cursor** (overwriting in `"rw"`/`"r+"`, never truncating) and advances it |
| `write_result(file, content: string) -> Result<int>` | Same, `value` = bytes written (`failure.message="stdin is read-only"` for `stdin()`) |
| `write_bytes(file, data: bytes) -> void` | Writes raw bytes at the cursor |
| `write_bytes_result(file, data: bytes) -> Result<int>` | Same, `value` = bytes written |
| `exists(path) -> bool` | Whether the path exists |
| `stat(path) -> FileInfo` | Size and `is_dir` (`exists=false` when the path is missing) |
| `remove(path) -> bool` | Deletes a file |
| `rename(src, dst) -> bool` | Renames/moves; `false` on failure |
| `mkdir(path) -> bool` | Creates the directory and any missing parent |
| `list_dir(path) -> IOLinesResult` | Entry **names** sorted by name, without the directory prefix and with no file/directory distinction — use `io.stat(path + "/" + name).is_dir` to tell them apart |

All reads and writes compose through the cursor: `read_line` then `write` (in
`"rw"`) overwrites right after the line just read; `seek` then `read_line`
reads from the new position; `read` after a few `read_line` calls returns the
**rest** of the file and leaves the cursor at the end, so a `read_line` after
it reports `EOF`; `read` right after `write` on the same `"rw"`/`"a"` handle
returns `""` (the cursor sits after what was written) — `seek(f, 0,
io.SEEK_SET)` rewinds. `stdin()` is one
non-seekable stream shared with `input()`; `read`/`read_lines`/`read_line`/
`read_n` all consume from it in order.

```noxy
use io

// K&R 8.4: get lê n bytes a partir da posição pos (b"" em erro)
func get(f: io.File, pos: int, n: int) -> bytes
    if io.seek(f, pos, io.SEEK_SET).ok then
        return io.read_n(f, n).data
    end
    return b""
end

let f: io.File = io.open("registros.bin", "rw")
let size: int = io.seek(f, 0, io.SEEK_END).position   // tamanho do arquivo
let rec: bytes = get(f, 64, 32)                          // registro do meio
io.seek(f, 64, io.SEEK_SET)
io.write_bytes(f, rec)                                   // sobrescreve no lugar
io.close(f)
```

### System (`sys`)

`sys.version` is the version of the Noxy running the program — the same
string `noxy --version` prints (`v0.23.4`). It is a module binding, not a
call: `use sys` then `print(sys.version)`, or `use sys select version`, which
brings it in typed as `string`.

`sys.exec_output(command, ...)` runs the command through the platform shell:
`sh -c` on Unix and **`cmd /C` on Windows**. The command string is therefore
already inside a `cmd` invocation — do not nest another `cmd /c ...`. The
captured output (stdout and stderr combined) is handed back as a Noxy `string`,
so it must be valid UTF-8: binary or non-UTF-8 output yields `ok=false` with
the UTF-8 error in `error`, even when the process exited with code 0.

### JSON

`json_dumps`, `json_parse` and `json_loads` are documented in
[`JSON_SUPPORT.md`](JSON_SUPPORT.md). `json_loads(text, ref target)` populates an
existing typed target in place and returns `false` (with no partial writes)
when the payload does not fit. "In place" keeps value semantics at every level:
a composite that is shared somewhere inside the target (`let copia = t[0]`
before the call) is cloned and the clone is written back to its parent —
exactly what `t[0].a = v` does — while a uniquely owned container mutates
without cloning. For a slot declared `ref T` **inside** the
target (array element, struct field, map value): a slot that already holds a
reference is written **through** it; a JSON `null` stores `null`; a non-null
payload for a slot that is `null` (or for a new element/field) builds the `T`
from the referent schema, allocates a fresh heap cell that owns it, and stores
a reference to that cell — afterwards `let viz: ref T = slot; type(viz)`
is `"ref"` and `*viz` reads the value. A `ref T` field or element passed
**directly** as the target while it is `null` arrives as `null` (§4.2) and
`json_loads` returns `false`; pass the owner instead (`json_loads(text, ref h)`).

### Strings

The `strings` module provides text manipulation functions. All index-based
string operations work on **Unicode rune (code point) positions**, not raw
bytes.

#### `substring(s, start, end_idx) -> string`

Returns the sub-string of `s` from rune index `start` (inclusive) to
`end_idx` (exclusive). Negative indices count from the end of the string
(Python-style: `-1` is the last rune, `-2` the second-to-last, etc.).
After resolving negatives, both indices are clamped to `[0, len]` where
`len` is the rune count of `s`. When `start >= end_idx` after resolution,
an empty string is returned.

```noxy
use strings
strings.substring("Hello", 1, 4)    // "ell"
strings.substring("Hello", 0, 2)    // "He"
strings.substring("Hello", 3, 100)  // "lo"   (end clamped to length)
strings.substring("Hello", -2, 5)   // "lo"   (-2 → index 3)
strings.substring("Hello", 0, -1)   // "Hell" (-1 → index 4)
strings.substring("Hello", -3, -1)  // "ll"   (-3 → 2, -1 → 4)
strings.substring("aé🙂z", 1, 3)   // "é🙂"  (rune-based, not byte-based)
```

#### Code points: `char_code`, `from_char_code`, `codes`

`char_code(s) -> int` returns the Unicode code point of a single-character
string (the `ord` of other languages); a string whose rune count is not
exactly 1 is a runtime error. `from_char_code(code) -> string` is its
inverse (`chr`). `codes(s) -> int[]` decodes the whole string once and
returns every code point — prefer it when scanning a string character by
character: `char_at` rebuilds the rune slice on each call, so a `char_at`
loop is quadratic in the string's length.

```noxy
use strings select *

char_code("A")            // 65
char_code("é")            // 233
from_char_code(233)       // "é"
codes("já")               // [106, 225]

// range comparison — no digit-table tricks needed
func is_ascii_digit(ch: string) -> bool
    let code: int = char_code(ch)
    return code >= 48 && code <= 57
end
```

String literals also accept `\u{...}` and `\uXXXX` escapes for writing a
character by its code point; `from_char_code(code)` is the runtime
equivalent for a code point computed at runtime.

### Indexação de strings

Uma `string` é indexada por **caractere** (code point Unicode), não por byte.
`length`, `substring`, `char_at`, `index_of`, `slice` e `reverse` usam todos a
mesma unidade, então compõem entre si:

```noxy
use strings select *

let nome: string = substring(linha, 0, index_of(linha, ":"))
```

`length("café")` é `4`, não `5`.

Um code point não é sempre um caractere percebido pelo usuário: `é` pode ser um
code point ou dois, e um emoji com modificador é vários. Só um modelo de
grapheme cluster seria exato, e ele não oferece índice inteiro em tempo
constante. Noxy adota a aproximação por code point, como Python.

`bytes` é o oposto: indexado por **octeto**, através de `length`, `slice` e
acesso por elemento. As funções de `strings` recusam um `bytes` e apontam
`to_str`, que é a ponte explícita entre os dois tipos.

### Invariante UTF-8

Toda `string` Noxy contém UTF-8 válido. A validação acontece uma única vez, na
fronteira onde bytes viram texto, e não em cada operação — dentro do
invariante, toda operação por caractere é correta por construção.

```noxy
use strings select *

let dados: bytes = io.read_bytes(arquivo).data
if is_valid_utf8(dados) then
    let texto: string = to_str(dados)
else
    print("conteúdo não é UTF-8")
end
```

`to_str` levanta erro de runtime sobre bytes inválidos, indicando o offset:

```text
to_str: bytes are not valid UTF-8 at byte offset 5
```

Funções que já possuem struct de resultado — `io.read`, `io.read_lines`,
`sqlite.query`, `sys.exec_output`, `sys.getenv` — reportam pelos campos `ok` e
de erro que já têm, em vez de levantar. Levantar fica reservado às conversões
puras.

Para lidar com bytes arbitrários deliberadamente, mantenha o valor como
`bytes`: `io.read_bytes` e `net.recv` já devolvem `bytes` e não validam nada.

`is_valid_utf8` aceita apenas `bytes`. Passar uma `string` — mesmo através de
`use strings select *`, o caminho que qualquer código real usa — levanta erro
de runtime nomeando o tipo recebido, porque o invariante já respondeu: se o
valor já é `string`, perguntar de novo é a pergunta errada.

O invariante vale em toda fronteira descrita neste documento, com uma exceção:
o script de entrada passado na linha de comando é lido por um caminho separado
do carregamento de módulos e ainda não é validado (registrado como trabalho
futuro no CHANGELOG).

Noxy não aplica normalização Unicode (NFC/NFD). Comparação é byte-exata, como
em Python — tanto a igualdade quanto a ordenação (`<`, `>`, `<=`, `>=`), que
ordena strings lexicograficamente por byte; dentro do invariante UTF-8 isso
é idêntico à ordem por code point.

### Network sockets

`net.listen(host, 0)` asks the operating system to choose an available port.
On success, the returned `Socket.port` is that assigned non-zero port, so it
can be passed directly to `net.connect` without inspecting native resources.

#### Persistent I/O deadlines

The `net` module supports portable blocking sockets with optional positive
deadlines:

```noxy
net.settimeout(sock, 250)
net.setblocking(sock, true)
```

`settimeout(sock, timeout_ms)` selects timed mode. `timeout_ms` must be a
positive `int` whose conversion to Go `time.Duration` milliseconds does not
overflow. Each `recv`, `send`, or listener `accept` starts with a fresh
deadline, so time spent between configuration and the operation does not
consume the operation's timeout.

`setblocking(sock, true)` clears the persistent timeout and restores
indefinite blocking, including I/O that is already pending. The compatibility
call `setblocking(sock, false)` is deprecated and remains an unconditional
no-op. Poll readiness does not depend on this compatibility call. It does not
inspect the socket or alter an existing timeout, and an expired deadline is
never used to simulate non-blocking behavior.

The latest successfully completed `settimeout` or `setblocking(..., true)`
call determines the persistent mode. A rejected call leaves that mode
unchanged. Configuration belongs to the shared network resource, so related
VMs observe the same mode.

For effective configuration calls, a socket may be the existing map-backed
`Socket` value or a typed `Socket` instance. The `fd` field must exist, be an
exact `int`, fit the platform descriptor type without narrowing, resolve to a
listener before a connected socket, and name an open resource. `fd` is
authoritative; mutable `open`, `addr`, and `port` fields are not trusted.
`settimeout` validates exact arity, timeout type and range, socket shape and
descriptor, lookup, then open state. `setblocking` retains its compatibility
behavior for malformed arity/type; only the exact `true` branch performs
socket validation.

#### Non-consuming readiness polling

`net.poll(read, write, error, timeout_ms)` (the public wrapper for
`net_select`) observes readiness without performing I/O. The three input sets
are independent fixed arrays with 64 entries each. For each set, only entries
0 through 63 are considered. The corresponding output is another 64-entry
array in stable input order; unused output entries are null. Duplicate socket
occurrences are retained as separate output occurrences. `read_count`,
`write_count`, and `error_count` are the exact numbers of values copied to
their respective outputs, not the number of distinct resources.

`timeout_ms` must be an `int` representable as Go `time.Duration`
milliseconds. A negative value is an error. Zero performs one immediate poll.
A positive value supplies one global wall-clock bound for the whole operation,
including every candidate in all three sets; it is not a per-socket or
per-set timeout. Poll may return earlier when readiness or a local concurrent
close is observed. With no live candidates, a positive poll waits for the same
global bound and returns empty sets.

Poll is strictly observational. It does not call `Read` or `Accept`, peek at
payload bytes, query or clear `SO_ERROR`, or install, clear, or restore a
socket deadline. It therefore neither consumes bytes nor accepts a pending
connection, and it does not mutate the persistent mode established by
`settimeout` or `setblocking(..., true)`.

For a connected socket, ordinary payload data is read readiness only. An
orderly EOF is always readable. Explicit native hangup, error, or invalid
descriptor events satisfy every set in which that connected socket was
requested. Payload data may coexist with EOF, hangup, or error readiness; poll
must not discard it, and a later `socket_recv` can still consume the pending
bytes. Out-of-band or urgent data is outside the `net.poll` API and has no
separate readiness category.

A listening socket with a pending connection has normal read readiness only.
A listener terminal event satisfies requested read and error sets, but a
listener is never returned in the write set. Poll never calls `Accept`, so a
pending connection remains available to a later explicit `net.accept`.

Closing a requested socket or listener concurrently in the same runtime wakes
a blocked positive poll. The detached resource is revalidated and omitted
from every output set; stale descriptor reuse cannot make the old resource
ready.

Windows and Linux receive runtime verification. The poller backend has compile
support for Windows and for `aix`, `darwin`, `dragonfly`, `freebsd`, `linux`,
`netbsd`, `openbsd`, and `solaris`; this is compile support, not a claim of
runtime verification on those Unix targets. Full-repository cross-build gates
currently cover `linux/amd64`, `darwin/amd64`, and `freebsd/amd64`; existing
SQLite dependencies prevent full-repository builds for the other listed Unix
targets even though the poller backend itself compiles there. On any platform
outside the Windows/Unix build-tag list, polling a live resource fails with
`network polling is not supported on this platform`; no consuming fallback is
used.

Accepted and connected sockets explicitly clear prior deadlines before being
registered and start in indefinite blocking mode; a listener timeout is not
inherited. Failure to clear that initial deadline closes the connection and
returns the existing `Socket{open:false}` result. Listener accept timeout also
uses that existing result shape.

Read and write expiry is reported as `"operation timed out"` using portable
deadline-error classification. A receive that obtained bytes before an error
remains successful with those bytes. A failed partial send reports `ok=false`
and its actual transferred `count`. If a multi-step configuration cannot
restore its exact prior absolute deadline snapshots after failure, the runtime
marks the shared resource closed, detaches its OS handles and buffers, and
closes them rather than leaving pending I/O in an unknown deadline state. The
poisoned entry remains invalid in the shared registry until ordinary
`net_close` removes the handle. If concurrent close invalidates a deadline
transition, that close owns cleanup; the stale transition cannot commit,
register, roll back, poison, or close the resource again.

---

## 13. Implementation Notes

- **VM**: Stack-based Virtual Machine.
- **Language**: Go.
- **Compilation**: Source (.nx) -> Bytecode (Chunk).
- **Execution**: The VM executes the bytecode instructions.

### Memory Model
- **Value Types**: Primitives (`int`, `float`, `bool`) are stored directly on the stack.
- **Heap-Backed Composite Types**: Objects (`struct`, `array`, `map`) are allocated on the heap.
    - **Variables**: Store a pointer to the heap object; sharing is managed by the copy-on-write runtime.
    - **Assignment**: Assigning a composite behaves as an independent deep copy (cloned lazily on first mutation).
    - **Function Calls**: A parameter without `ref` receives an independent value at any depth through value fields (copy-on-write); a `ref` field inside it is a shared edge (§2.2 rule 6).
    - **Reference Parameters**: A parameter declared with `ref` shares the caller's slot — `ref`, as parameter or as field, is the only sharing mechanism.

### Call and operand stacks

Both stacks are **grown on demand**, per VM. Each VM starts with 64 call frames
and 4096 operand slots and doubles them as needed up to the caps of **100 000
frames** and **1 048 576 operand slots** — so recursion depth is bounded by
memory, not by a small fixed array, and a task or `spawn` still starts cheap.
Reaching a cap is an ordinary runtime error (`stack overflow: call depth
exceeds 100000 frames` / `stack overflow: operand stack exceeds 1048576
slots`), never a Go panic; see §7, *Limites de chamada*.

---
*Version: 0.13.0*
*Language: Noxy*
*Implementation: Stack VM (Go)*

<!-- {% endraw %} -->
