# Noxy Language Specification

## Overview

Noxy is statically typed, with explicit dynamic boundaries through `any`, bare
`func`, untyped native primitives, and plugins without signatures. Designed for
educational purposes and practical applications, it supports structs,
references, arrays, f-strings, and a module system.
The current implementation is a **Stack-based VM** written in **Go**.

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
| Control Flow | `if`, `elif`, `then`, `else`, `end`, `while`, `do`, `return`, `break`, `continue`, `for`, `in`, `defer`, `when`, `case`, `default` |
| Types | `int`, `float`, `string`, `bool`, `void`, `ref`, `bytes`, `func` |
| Literals | `true`, `false`, `null` |
| Modules | `use`, `select`, `as` |
| Specials | `zeros` |

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

---

## 2. Type System

### 2.0 Fundamental Typing Rules

#### Static Typing and Type-Stable Variables

Noxy uses **type-stable variables**: a variable's declared type remains stable,
while the value stored in it may be mutable.

1. **The type of a variable is defined at declaration and cannot be changed.**
2. Attempts to assign a value of a different type result in a compilation error.
3. There is no implicit conversion between types (except where explicitly documented).

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
`ref` behaves as an independent deep copy, at any depth:

1. **Assignment copies**: `let b = a` and `x = y` produce independent values.
2. **Calls copy**: arguments to non-`ref` parameters are independent values —
   nested mutation inside the callee never leaks to the caller.
3. **Reading from a container copies**: `let p = arr[0]` produces an
   independent value; mutating `p` does not affect `arr[0]`.
4. **Storing into a container copies**: `append(outer, inner)`, `m[k] = v`,
   `s.field = arr`, and constructor arguments store independent values.
5. **Channels carry values**: `chan_send` delivers an independent copy. This
   applies to `spawn` and `spawn_task` arguments equally.
6. **`ref` is the only sharing mechanism.** A `ref` points to a *slot*
   (variable, field, index, map entry); writes through any alias of the slot
   are visible to all aliases of that slot.
7. **`==`/`!=` on composites is structural** (recursive by content). `ref`
   values compare by slot identity and are not dereferenced — both as fields
   nested inside a composite and as the two operands of a direct comparison.
   A `ref` compared against `null` asks whether the reference *itself* is
   null, and a `ref` compared against a plain value is a compile-time error:
   the read must be explicit (`*r == value`); see §2.3.
8. Closures capture *variables* (slots); captured-variable aliasing is
   unchanged and orthogonal to value semantics.

The runtime implements this contract with **copy-on-write**: no copy is made
at the binding site — composites are marked as shared and cloned lazily, one
level at a time, at the first mutation. Read-only sharing therefore costs
O(1); programs never observe the difference, only the performance.

One documented edge: a `ref` taken *into* a container (`ref arr[0]`, a `ref`
field) pins that container's identity at creation time. If the container is
copied *afterwards*, writes through the pre-existing `ref` are visible to
copies that have not yet materialized. Take refs after, not before, sharing.

#### Concurrency and composite values

Shared routines use synchronized global bindings, module state, maps, and runtime handle registries. An individual binding lookup/update or map operation is safe from the Go runtime's concurrent-map crash, but synchronization is not recursive and does not make a read-modify-write sequence atomic. Normal calls, `spawn`, and `spawn_task` all follow the value-semantics parameter rules above — the legacy `spawn` identity exception was removed in 0.4.0 — so data handed to another routine by argument or channel is race-free by construction. Concurrent mutation of intentionally shared state (globals, `ref`) still requires coordination through channels or another explicit single-owner protocol.

#### Arrays (Dynamic and Fixed)

**1. Dynamic Arrays (Recommended)**
```noxy
// Declaration (starts empty)
let dynamic: int[] 

// Operations
append(dynamic, 10)
length(dynamic)
```

**2. Fixed Size Arrays**
```noxy
let fixed: int[5] = [1, 2, 3, 4, 5]
let zeroed: int[100] = zeros(100)
```

**Pass-by-Value Behavior**:
Arrays are passed by **VALUE**: the callee's array is independent at any depth (copy-on-write). Use `ref` when the function must modify the caller's array.

#### Maps (Hashmaps)

```noxy
// Type: map[Key, Value]
let scores: map[string, int] = {"Alice": 100}
scores["Bob"] = 50
```

**Pass-by-Value Behavior**:
Maps are passed by **VALUE**: the callee's map is independent at any depth (copy-on-write). Use `ref` when the function must modify the caller's map.

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
Structs are passed by **VALUE**: the callee's instance is independent at any depth (copy-on-write), including nested composite fields. Use `ref` when the function must modify the caller's original instance.

---
### 2.3 References (`ref`)

#### The `ref` Operator

The `ref` operator produces an explicit first-class reference value according
to the operand's type:

1. For an addressable operand of type `T`, `ref value` creates a `ref T` that
   points to that operand's storage.
2. For an operand whose type is already `ref T`, `ref reference` forwards the
   existing reference value. Its result remains `ref T`; it does not take the
   address of the reference variable or create `ref ref T`.

The forwarding form is useful when an existing reference crosses a dynamic
boundary whose signature is not available for contextual conversion.

#### L-Value Requirement
Creating a new reference requires an **addressable value** (L-Value). The
operand must be a variable, a struct field, or an array/map index. A non-reference
temporary, such as an ordinary function result or a literal, is not
addressable. Forwarding an expression that already has type `ref T` does not
create a new reference and therefore does not require a second storage slot.

Captured variables are addressable through their upvalue storage. Non-null
literals and plain function-result temporaries are not addressable. A function
result whose declared type is already `ref T` is a reference value and may be
passed directly. `null` remains the explicit nullable `ref T` value: it is
accepted without pretending that it owns a storage slot.

**Correct Usage:**
```noxy
let err: Error = Error("msg")
let r: ref Error = ref err      // OK: 'err' is a variable
let forwarded: ref Error = ref r // OK: forwards the existing ref Error
```

**Incorrect Usage:**
```noxy
let r: ref Error = ref Error("msg") // ERROR: Cannot take reference of temporary value
```

#### Reference Semantics
The `ref` keyword creates references to addressable values or explicitly
forwards existing reference values. Noxy unifies reference usage through
**"Automatic Dereference"** and **"Type-Based Assignment"**.

#### 1. Automatic Dereference (Expressions)
You can use a reference (`ref T`) in expressions just like a normal value. The compiler automatically assumes you want the **value**.
```noxy
let x: int = 10
let r: ref int = ref x

// 1. Reading (Auto-Dereference)
// You can use the reference directly to READ the value.
// The compiler automatically follows the pointer.
let y: int = r + 1   // Compiler auto-derefs 'r' -> 11
print(r)             // Prints 10
```
This applies to both Local Variables and Struct Fields.

Auto-dereference has exactly **two exceptions**, described below: `==`/`!=`
with a reference operand, and the right-hand side of a plain assignment.

**Exception 1: `==` and `!=` with a reference operand.** Auto-dereference
answers "what value is there?", which is the wrong question for equality on
references — there the questions are about *identity* and *nullity*. In
`==`/`!=` a reference operand is **never** implicitly dereferenced:

- **two references** compare by **slot identity** (§2.2, rule 7);
- a reference compared against **`null`** asks whether the reference
  *itself* is null — which keeps `node.next != null` working, and makes a
  valid reference to a slot that *contains* null distinguishable from a
  null reference (`r == null` asks about the ref, `*r == null` about the
  pointed value — two questions that implicit dereference used to conflate);
- a reference compared against a **plain value** is a compile-time error
  with a hint — write the read explicitly, as in assignment.

```noxy
let a: int = 1
let b: int = 1
let ra: ref int = ref a
let rb: ref int = ref b
let ra2: ref int = ref a

ra == ra2   // true  — same slot
ra == rb    // false — different slots, even though both hold 1
*ra == 1    // true  — explicit dereference reads the pointed value
ra == 1     // ERROR: cannot compare ref int with int: a ref is never
            //        implicitly dereferenced in '=='
            //   hint: use '*ra' to compare the referenced value
ra == null  // false — the reference itself is valid

node.next != null   // unchanged: asks whether the `ref` field is null
```

`addr(ref x)` remains available when you want that identity as a printable
value rather than a comparison.

**Exception 2: the right-hand side of a plain assignment.** Assignment is
where *update* and *rebind* are told apart by the static types of both sides
(see the summary table below), so around `=` no reference conversion is
implicit — in either direction. Assigning a `ref T` to a target that expects
a plain `T` (a variable, an array/map entry, or a struct field) is a
compile-time error with a hint, not an implicit read:

```noxy
let x: int = 10
let r: ref int = ref x
let n: int = 0

n = r    // ERROR: type mismatch in assignment to 'n': expected int, got ref int
         //   hint: use '*r' to read the referenced value
n = *r   // OK: explicit dereference reads 10
```

The explicit-update form is different: in `*r = value` the `*` already names
the target unambiguously, so a reference RHS keeps the ordinary expression
rule and is read. With `s: ref int`, `*r = s` writes the value `s` points to
(equivalent to `*r = *s`).

Note that `let` initialization is *not* an assignment in this sense: a `let`
creates a fresh slot, so there is nothing to rebind and no ambiguity —
`let n: int = r` auto-dereferences, like any other expression position
(including exact call arguments; see §4.2).

#### 2. Writing (Update vs Rebind)
The distinction between modifying the *value* and modifying the *pointer* is made explicit by syntax:

**A. Value Update (Explicit `*`)**
To update the content of the memory pointed to by a reference, you MUST use the dereference operator `*`.
```noxy
*r = 20      // DESTROY/UPDATE: Writes 20 into the memory of 'x'
*box.val = 30 // Writes 30 into the memory pointed to by 'box.val'
```

**B. Pointer Rebind (Standard `=`)**
To change the reference itself (make it point to something else), use standard assignment.
*Note: The type of the RHS must be a Reference (`ref T`).*
```noxy
let z: int = 99
r = ref z    // REBIND: 'r' now points to 'z' (does not affect 'x')
```

#### 3. Strict Type Safety
The compiler enforces these rules to prevent ambiguity, and each rejection
points at the intended fix:
```noxy
let n: int = 0
r = 50       // ERROR: cannot assign int to ref int
             //   hint: use '*r = ...' to update the referenced value
n = r        // ERROR: type mismatch in assignment to 'n': expected int, got ref int
             //   hint: use '*r' to read the referenced value
n = *r       // OK: explicit dereference
*r = ref z   // OK: '*' names the target unambiguously, so the reference RHS
             // is read — writes z's value through r (same as '*r = z')
```

#### 4. Reference Patterns

These patterns allow Noxy to safely support smart pointers and mutable bindings.

##### Pattern A: Mutable Bindings (Pass-by-Reference)

Functions can modify external variables through references:

```noxy
func double_it(val: ref int)
    *val = val * 2  // UPDATE: writes to original variable
end

func swap(a: ref int, b: ref int)
    let val_a: int = a  // Read values (auto-deref)
    let val_b: int = b
    *a = val_b          // UPDATE: write to address of 'a' using '*'
    *b = val_a          // UPDATE: write to address of 'b' using '*'
end

let x: int = 10
double_it(x)      // exact signature borrows addressable x; x is now 20
double_it(ref x)  // explicit reference is also valid; x is now 40

let a: int = 100
let b: int = 200
swap(ref a, ref b)  // a=200, b=100
```

> **Note**: This syntax makes swaps safe and explicit. `a = b` would try to rebind the pointer `a` to point to the same place as `b` (if `b` were a reference expression), which is not what you want in a swap.

##### Pattern B: Dynamic Aliases

A local reference can be rebound to point to different variables:

```noxy
let counter_A: int = 0
let counter_B: int = 0

let active: ref int = ref counter_A

*active = *active + 1     // Updates counter_A (now 1)
active = ref counter_B    // REBIND: now points to counter_B
*active = *active + 1     // Updates counter_B (now 1)
// Result: counter_A=1, counter_B=1
```

##### Pattern C: Smart Pointers (Observer Pattern)

Structs with reference fields can dynamically switch their data source:

```noxy
struct Observer
    name: string
    target: ref int
end

let temperature: int = 20
let humidity: int = 50

let sensor: Observer = Observer("Main", ref temperature)

// Read through reference
print(sensor.target)  // 20 (auto-deref)

// UPDATE value
*sensor.target = 25   // temperature is now 25

// REBIND to different source
sensor.target = ref humidity  // Now watching humidity
*sensor.target = 70           // humidity is now 70
```

##### Summary Table: Type-Based Assignment

| LHS Type | RHS Type | Syntax | Action |
|----------|----------|--------|--------|
| `ref T` | `T` | `*r = val` | **UPDATE** – writes into memory |
| `ref T` | `ref T` | `r = ref x`| **REBIND** – changes pointer |
| `T` | `T` | `x = val` | Standard assignment |
| `T` | `ref T` | `x = *r` | **READ** – explicit dereference required; plain `x = r` is a compile error (§2.3, exception 2) |

The field rules apply identically when the assignment **base** is itself a
reference: with `node: ref Node`, `node.valor = "texto"` is `type mismatch in
field assignment: expected int, got string`, and `node.proximo = Node(9, null)`
is `cannot assign Node to ref Node` — the compiler resolves the field through
the dereferenced base and checks it exactly as it checks `a.valor` /
`a.proximo` with `a: Node`. For a `ref T` field, array element, or map value
the error names the two legitimate paths:

```
hint: to point the field at a new value, bind it to a variable first and use 'x.proximo = ref novo'; to overwrite the referenced value use '*x.proximo = ...'
```

#### Memory Safety (Captured Variables)
Noxy ensures memory safety when using `ref`.
- If you create a `ref` to a **local variable**, that variable is automatically **Captured** (moved to the Heap) by the compiler.
- Implemented via **Upvalues**, this ensures that the variable survives the end of the function scope.

```noxy
func create_safe_ref() -> ref int
    let x: int = 42
    return ref x // Safe! 'x' is promoted to Heap because it is referenced.
end
```

---

## 3. Variable Declarations

```noxy
let name: type = value
```

Variables can be reassigned, but the new value **MUST** be of the same type as declared.

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

When an exact parameter is `ref T`, an addressable expression of type `T` is
converted contextually at the call site. Both the concise and explicit forms
are valid:

```noxy
let value: int = 10
double_it(value)       // exact signature borrows addressable value
double_it(ref value)   // explicit reference is also valid
```

This contextual conversion is limited to exact script signatures and public
native contracts known by the compiler. It never applies at a dynamic
boundary.

An argument whose static type is already `ref T` — a `ref` variable, a struct
field, an array element, or a map value declared `ref T` — needs no
conversion: the stored reference is forwarded as is, **including `null`**,
exactly as a `ref` variable is (§2.3, rule 2). No reference to the containing
slot is created, so inside the callee `param == null` is true precisely when
the stored reference was null, and writing through such a parameter is the
ordinary null-reference error. The same forwarding applies to every position
that takes a reference contextually — the explicit form `ref a.field`, a
constructor argument for a `ref` field (`Node(2, a.next)`), `return ref
n.field`, `append` into a `(ref T)[]`, and the target of `json_loads`. To
fill an empty `ref` field, the callee receives the *owner* and rebinds the
field:

```noxy
func append_node(node: ref Node, valor: int)
    if node.proximo == null then
        let novo: Node = Node(valor, null)   // a variable: `ref` needs an L-value
        node.proximo = ref novo              // REBIND of the owner's field
    else
        append_node(node.proximo, valor)     // forwards the stored reference
    end
end
```

A slot declared `ref T` always holds a reference or `null`, and the runtime
never wraps anything else: should a slot hold a raw `T` (which no Noxy program
can produce), forwarding it is the explicit error `reference slot 'field'
holds a non-reference value`. Through a base typed `any` the same forwarding
applies — `ref a.proximo`, `f(a.proximo)`, `json_loads(s, a.proximo)` with
`a: any` forward the stored reference or `null` exactly as the typed base does
— and writing a raw `T` into a `ref T` field, element, or map value through
`any` is the runtime error `cannot assign T to ref T`.

Bare `func` is the **dynamic callable type**. It guarantees only that the value is callable:

```noxy
let dynamic: func = exact       // exact-to-dynamic widening is valid
let exact_again: func(int) -> int = dynamic // ERROR: no implicit narrowing
```

Calls through bare `func` are checked by the runtime because their arity and result type are not statically known. Their compile-time result is `any`, so a function returning their result must either declare `any` or replace the bare callable with an exact signature. Explicit `ref` arguments remain references across this dynamic boundary. This keeps dynamic callbacks, decorators, handlers, and heterogeneous callable collections available without pretending their results are statically known:

```noxy
let callbacks: func[] = [no_arguments, two_arguments]
```

Because bare `func` has no exact signature, a reference argument must be
written explicitly as `ref value`; `dynamic(value)` passes a plain value and
never infers or manufactures a reference. The same rule applies to untyped
native primitives, plugins without signatures, and dynamic module members.

Use parentheses for an array whose elements are exact functions. Without parentheses, the array belongs to the return type:

```noxy
let transforms: (func(int) -> int)[] = [double, increment]
let factory: func(int) -> int[]
```

Exact function types may also appear in parameters, returns, struct fields, map values, channels, and references. `any`, native functions, plugins, and untyped module exports remain dynamic boundaries and retain runtime validation; an unknown native value cannot be implicitly narrowed to an exact function type.

### 4.3 Parameter Passing Semantics (CRITICAL)

Noxy uses **Pass-by-Value** by default. Primitive values are copied directly. Composite values (arrays, maps, and structs) behave as independent deep copies at any depth, implemented with copy-on-write (see §2.2).

#### Pass-by-Value (Default)

When a composite value is passed to a parameter without `ref`, the function's view is fully independent of the caller's — mutating it at any depth never affects the caller:

```noxy
func modify(arr: int[]) -> void
    append(arr, 999) // Callee's value only
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

#### Pass-by-Reference (`ref`)
To share the caller's value and let the function mutate it, use `ref` in the parameter type — the only sharing mechanism in the language.

```noxy
func modify(arr: ref int[]) -> void
    append(arr, 999) // Modifies the ORIGINAL list
end

let list: int[] = [1, 2, 3]
modify(list)
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

### Self-Reference
Structs can reference themselves using `ref`.

```noxy
struct Node
    value: int
    next: ref Node
end
```

A `ref` field is filled by rebinding it to a variable (`let novo: Node = ...;
node.next = ref novo`) or cleared with `null`; assigning a raw `Node` to it is
a compile error whether `node` is a `Node` or a `ref Node` (§2.3, §4.2).

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
    append(s.items, item)
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

This works because every `let` in Noxy already requires a type annotation —
the language already forces the caller to write the information inference
needs.

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
        append(out, fn(item))
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

- **`T` cannot bind a `ref` type.** Passing a `ref` value where `T` would
  bind to `ref X` is a compile-time error; declare the parameter as `ref T`
  instead when the generic needs to receive a reference:

  ```noxy
  func identity<T>(x: T) -> T
      return x
  end
  let r: ref int = null
  let v: ref int = identity(r)   // ERROR: T cannot be a ref type

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

A `ref bool` condition is fine — `if r then` dereferences automatically — but
`&&`/`||` never dereference, so `r || x` is a compile-time error; write
`*r || x`.

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

A **result struct** means the input was allowed to be bad: untrusted text,
user input, wire data. Failure is an expected outcome, so it is data — an
`ok` flag the caller must branch on. Because Noxy functions return a single
value, the result struct occupies the place of Go's `value, err` pair.

API design rule: an operation whose failure indicates a caller bug raises; an
operation whose failure is an expected outcome of untrusted data returns a
result struct. When both kinds of caller exist, provide the raising form and a
`_result` twin: `to_int` / `to_int_result`, `io.close` / `io.close_result`.

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

let r: CallResult = call_result(to_int, entrada)
if r.ok then
    let n: int = to_int(r.value)   // narrowing only: r.value is already int
    print(n)
else
    print("entrada inválida: " + r.failure.message)
end
```

**Signature.**

```noxy
call_result(fn, ...args)   // -> CallResult
```

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

let c: CallResult = call_result(Ponto, 3, 4)      // constructor: value is the instance
let p: any = c.value                               // p.x == 3, p.y == 4

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

**Envelope.** `call_result` always returns a `CallResult` (module `errors`);
it never raises for anything that happens *inside* `fn`:

| Field | Type | `fn` completes | Failure captured |
|---|---|---|---|
| `ok` | `bool` | `true` | `false` |
| `value` | `any` | `fn`'s return value; `null` for `void` | `null` |
| `failure` | `Failure` | `null` | the primary failure |

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

**Representation.** `call_result` is a native, and natives return values
across the dynamic boundary (§4.2). Like every result envelope a native
produces today (`convert_to_int_result` and its `IntResult`), the envelope is
physically a map whose fields match the declared shapes; the `errors` module
declarations exist so Noxy code can annotate (`let r: CallResult = ...`) and
so the field names are a compile-checkable contract at typed use sites. The
consequences are observable and identical to the existing `IntResult`
precedent: `fmt("%T", r)` reports `map`, and the envelope does not compare
equal to a hand-constructed `CallResult(...)` instance. Promoting the
envelopes (this one and `task_await`'s) to genuine struct instances is a
single future change, gated on natives being able to construct
stdlib-declared structs. The annotation is nonetheless honored, and by a
general rule rather than a special case for this envelope: wherever a struct
shape is expected at a dynamic-boundary annotation or marker, the runtime
type validator admits any structurally matching map — every field the struct
declares present, each recursively compatible with the declared field type —
without nominal checking, and without stamping the map as that struct. The
admission stops at nominal gates: task-argument validation (`spawn_task`) and
every other check that requires a real struct instance still reject a map, so
a `CallResult` envelope passed as a typed task argument fails today;
unifying the two matchers is tracked as follow-up.

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
let r: CallResult = call_result(func() -> int
    return to_int(campo) * fator
end)
```

but the idiom is wrap-and-name: a `_result` function with a typed result
struct is a contract; an inline boundary is a site the reader must decode. The
boundary's design leans on auditability — `call_result` is one grep away from
every place errors become data — and on the envelope being deliberately
untyped (`value: any`), so a named twin that narrows the value is always the
more comfortable form. Discarding the envelope — `call_result(f, x)` as a
bare statement — swallows every failure `f` can produce and is almost
certainly a bug; a compile-time diagnostic for it is tracked as follow-up.

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

### Comparison
`>`, `<`, `>=`, `<=`, `==`, `!=`

Ordering (`>`, `<`, `>=`, `<=`) is defined for **numbers** (with the usual
int/float promotion) and for **strings**. String ordering is lexicographic
and byte-exact — for valid UTF-8, which every Noxy string is by invariant,
this is identical to code-point order, mirroring Python. Ordering any other
operand pair, including `bytes`, is a runtime error (`operands must be
numbers or strings`); bridge bytes through `to_str` first. Equality
(`==`, `!=`) is structural for every type (§2.2, rule 7).

### Logical
- `&&` (AND)
- `||` (OR)
- `!` (NOT)

All three require `bool` operands — there is no truthy/falsy conversion (§7).
With a known static type that is not `bool` the program is rejected at compile
time (`operand of '!' must be bool, got int`, `logical operators require
boolean operands, got int and bool`); an `any` operand is checked at runtime.
`&&` and `||` do **not** dereference: with `r: ref bool`, `if r then` works but
`r || x` is an error — write `*r || x`.

### Bitwise
- `&` (AND)
- `|` (OR)
- `^` (XOR)
- `~` (NOT)
- `<<`, `>>` (Shift)

The bitwise operators are strictly bitwise: they are never a substitute for
`&&`/`||`. `&`, `|` and `^` accept two `int` or two `bytes` of the same length;
`<<` and `>>` accept `int` only; `~` accepts `int` only. Wrong static types are
compile-time errors with the same text as the runtime check
(`[line N] operands for & must be integers or bytes, got int and bool`,
`operand of '~' must be int, got bool`).

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
call itself (see the quoting rule below): `f'{fmt("%10s", x)}'`.

**Double quotes inside `{}`** close an `f"..."` string: use a single-quoted
f-string when the interpolated expression contains a string literal.

```noxy
print(f'{"a"}')               // a
```

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

let porta: IntResult = to_int_result(getenv("PORT").value)
if porta.ok then
    print("porta " + to_str(porta.value))
else
    print("PORT inválida: " + porta.error)
end
```

Validar antes de converter não é uma alternativa correta: `is_digit` aceita
`"9999999999999999999"`, que estoura `int64`, e não há como checar o intervalo
sem converter. Não existe `is_float`.

### Collections
- `length(arr_or_map)`
- `append(arr, val)`
- `pop(arr)`
- `keys(map)`: Returns array of keys.
- `has_key(map, key)`: Returns bool.
- `delete(map, key)`

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
use is inspecting `any` values at dynamic boundaries (`call_result` and
`task_await` envelopes, JSON, channel payloads). The names:

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
`call_result` envelope reports `"map"` (its physical shape at the dynamic
boundary), and any value bound to `any` reports what it actually is. The
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

Supervised tasks share globals, module state, runtime resources, closure environments, and the VM configuration. Ordinary composite arguments follow value semantics (independent at any depth, copy-on-write) and `ref` arguments retain reference identity. Intentionally shared state — globals, `ref`, closure upvalues — still requires explicit concurrency coordination.

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

### Module state is read-only from outside

Assigning to a module variable through the namespace is a compile-time error:

```text
[line 7] cannot assign to 'counter.total': module variables are read-only outside the module
  hint: expose a function in 'counter' that updates it
```

Expose a function in the module that performs the update. (The rule covers the
direct form `m.x = v`; a global `let` that shadows the namespace name is a
different binding and is unaffected.)

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
file` when the namespace itself is unknown.)

---

## 12. Standard Library

Noxy comes with a comprehensive standard library. Available modules include:

| Module | Description |
|--------|-------------|
| `io` | Input/Output operations (read/write files) |
| `strings` | String manipulation (upper, lower, replace, split) |
| `time` | Time and Date functions |
| `sys` | System interactions (argv, exit, env) |
| `net` | Network sockets (TCP/UDP) |
| `http` | HTTP Client and Server |
| `json` | JSON parsing and stringification |
| `crypto` | Cryptographic functions (hashing, UUID) |
| `sqlite` | SQLite database support |
| `rand` | Random number generation |
| `errors` | Error boundary envelope shapes (Failure, CallResult) |

### I/O (`io`)

Every fallible operation reports through a result struct instead of raising
(§7, *Errors: raise for bugs, results for data*):

| Struct | Fields |
|--------|--------|
| `File` | `fd: int`, `path: string`, `mode: string`, `open: bool` |
| `IOResult` | `ok: bool`, `data: string`, `error: string` |
| `IOBytesResult` | `ok: bool`, `data: bytes`, `error: string` |
| `IOLinesResult` | `ok: bool`, `data: string[]`, `error: string` |
| `IOWriteResult` | `success: bool`, `bytes_written: int`, `error: string` |
| `IOCloseResult` | `success: bool`, `error: string` |
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
| `close_result(file) -> IOCloseResult` | Same, reporting the outcome (`success=false`, `error="stdin cannot be closed"` for `stdin()`) |
| `read(file) -> IOResult` | Everything **from the cursor to the end** as text (the whole file on a fresh handle; what is left after `read_line`/`read_n`/`seek`/`write`; `""` with `ok=true` when already at the end). Leaves the cursor at the end |
| `read_lines(file) -> IOLinesResult` | Same range split by line, `\r\n` normalized, **with no trailing `""`**: `"a\nb\n"` and `"a\nb"` both give `[a, b]`; `""` gives `[]` |
| `read_bytes(file) -> IOBytesResult` | Same range as raw `bytes`, no UTF-8 validation |
| `read_line(file) -> IOResult` | **Incremental**: the next line from the cursor, without `\r\n`. At end of file `ok=false, data="", error="EOF"`; a last line with no `\n` is returned normally and the next call reports EOF |
| `read_n(file, n) -> IOBytesResult` | **Incremental**: up to `n` raw bytes from the cursor. Fewer than `n` only at the end of the file; nothing left gives `ok=false, error="EOF"`; `n = 0` gives `b""` with `ok=true`; `n < 0` is an error. Works on `stdin()` |
| `seek(file, offset, whence) -> IOPositionResult` | Moves the cursor to `offset` bytes from the start (`SEEK_SET`), from the current position (`SEEK_CUR`) or from the end (`SEEK_END`) and reports the new absolute position. Past the end is allowed (a later read reports EOF; a write extends the file). Errors: `"stdin is not seekable"`, `"invalid whence N (...)"`, a negative resulting position (cursor unchanged), `"File not open"` |
| `tell(file) -> IOPositionResult` | The current cursor position (`"stdin is not seekable"`, `"File not open"`) |
| `write(file, content: string) -> void` | Writes text **at the cursor** (overwriting in `"rw"`/`"r+"`, never truncating) and advances it |
| `write_result(file, content: string) -> IOWriteResult` | Same, reporting `bytes_written` (`error="stdin is read-only"` for `stdin()`) |
| `write_bytes(file, data: bytes) -> void` | Writes raw bytes at the cursor |
| `write_bytes_result(file, data: bytes) -> IOWriteResult` | Same, reporting `bytes_written` |
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

`sys.exec_output(command, ...)` runs the command through the platform shell:
`sh -c` on Unix and **`cmd /C` on Windows**. The command string is therefore
already inside a `cmd` invocation — do not nest another `cmd /c ...`. The
captured output (stdout and stderr combined) is handed back as a Noxy `string`,
so it must be valid UTF-8: binary or non-UTF-8 output yields `ok=false` with
the UTF-8 error in `error`, even when the process exited with code 0.

### JSON

`json_dumps`, `json_parse` and `json_loads` are documented in
[`JSON_SUPPORT.md`](JSON_SUPPORT.md). `json_loads(text, target)` populates an
existing typed target in place and returns `false` (with no partial writes)
when the payload does not fit. For a slot declared `ref T` **inside** the
target (array element, struct field, map value): a slot that already holds a
reference is written **through** it; a JSON `null` stores `null`; a non-null
payload for a slot that is `null` (or for a new element/field) builds the `T`
from the referent schema, allocates a fresh heap cell that owns it, and stores
a reference to that cell — afterwards `let viz: ref T = slot; type(ref viz)`
is `"ref"` and `*viz` reads the value. A `ref T` field or element passed
**directly** as the target while it is `null` arrives as `null` (§4.2) and
`json_loads` returns `false`; pass the owner instead (`json_loads(text, h)`).

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
    - **Function Calls**: A parameter without `ref` receives an independent value at any depth (copy-on-write).
    - **Reference Parameters**: A parameter declared with `ref` shares the caller's slot — the only sharing mechanism.

### Call and operand stacks

Both stacks are **grown on demand**, per VM. Each VM starts with 64 call frames
and 4096 operand slots and doubles them as needed up to the caps of **100 000
frames** and **1 048 576 operand slots** — so recursion depth is bounded by
memory, not by a small fixed array, and a task or `spawn` still starts cheap.
Reaching a cap is an ordinary runtime error (`stack overflow: call depth
exceeds 100000 frames` / `stack overflow: operand stack exceeds 1048576
slots`), never a Go panic; see §7, *Limites de chamada*.

---
*Version: 0.11.0*
*Language: Noxy*
*Implementation: Stack VM (Go)*
