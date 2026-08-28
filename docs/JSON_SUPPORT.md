# JSON Support in Noxy

Noxy provides native support for high-performance JSON serialization and deserialization directly in the VM. This functionality allows seamless integration with REST APIs and configuration files.

## Native Functions

| Function | Direction | Description |
| :--- | :--- | :--- |
| `json_dumps` | **Noxy -> String** | Serializes a value into a JSON string. |
| `json_parse` | **String -> Noxy** | Parses a JSON string into a new generic value. |
| `json_loads` | **String -> Noxy** | Parses a JSON string into an *existing* typed variable. |

### 1. Serialization (Noxy -> String)

**Function:** `json_dumps`

Converts a Noxy value (Struct, Map, Array, or Primitive) into its JSON string representation.

**Signature:**
```noxy
func json_dumps(val: any) -> string
```

**Supported Types:**
- **Primitives:** `int`, `float`, `bool`, `string`, `null` are converted to their JSON equivalents.
- **Maps:** Converted to JSON Objects. Keys are converted to strings.
- **Arrays:** Converted to JSON Arrays.
- **Struct Instances:** Converted to JSON Objects using their field names.

**Example:**
```noxy
struct User
    name: string
    active: bool
end

let u: User = User("Alice", true)
let json: string = json_dumps(u) 
// Result: '{"active":true,"name":"Alice"}'
```

---

### 2. Deserialization (String -> Noxy)

We provide two ways to convert a JSON string back into a Noxy value:

#### A. Generic Parsing: `json_parse`

Creates a **new** Noxy value from a JSON string. Use this when you don't have a specific destination struct or when the data structure is dynamic.

**Signature:**
```noxy
func json_parse(json_str: string) -> any
```

**Returns:** 
- The parsed value (Map, Array, or Primitive).
- `null` if parsing fails.

**Example:**
```noxy
let json: string = "{\"id\": 1, \"tags\": [\"a\", \"b\"]}"
let data: any = json_parse(json)

// data is a generic Map: 
// { "id": 1, "tags": ["a", "b"] }
```

#### B. Targeted Loading: `json_loads` (formerly unmarshal)

Populates an **existing** typed object (like a Struct) with data from a JSON string. This is safer and strictly typed.

**Signature:**
```noxy
func json_loads(json_str: string, target: any) -> bool
```

**Arguments:**
1. `json_str`: The JSON string to parse.
2. `target`: A reference to the object (Struct instance, Map, or Array) to populate.

**Returns:** 
- `true` if successful.
- `false` if parsing failed.

**Example:**
```noxy
struct Config
    port: int
    debug: bool
end

// 1. Initialize empty struct
let conf: Config = Config(0, false) 

// 2. Load JSON directly into 'conf'
let json: string = "{\"port\": 8080, \"debug\": true}"
json_loads(json, ref conf)

print(conf.port) // 8080
```

### Reference slots (`ref T`)

A slot declared `ref T` inside the target — `(ref T)[]` element, `ref T` struct
field, `map[K, ref T]` value — always ends up holding a reference or `null`:

| Slot before | JSON payload | Result |
| :--- | :--- | :--- |
| reference | non-null | written **through** the reference (the referent changes) |
| reference or `null` | `null` | slot becomes `null` |
| `null`, or a new element/field | non-null | `T` is built from the referent schema, a fresh heap cell owns it, and the slot gets a reference to the cell — like `let novo: T = ...; slot = ref novo` |

```noxy
let target: (ref int)[] = [null]
json_loads("[42]", ref target)
let viz: ref int = target[0]
print(type(viz))   // ref
print(*viz)         // 42
```

Passing a `null` `ref T` field or element **directly** as the target
(`json_loads(s, h.child)` — `h.child` is already `ref T`, so no `ref` at the
call site) forwards the stored `null`; there is no slot behind it, so the
call returns `false`. Pass the owner instead (`json_loads(s, ref h)`) or
point the slot first.

## Type Mapping

| Noxy Type | JSON Type | Notes |
| :--- | :--- | :--- |
| `int`, `float` | Number | Numbers are converted to `float` or `int` appropriately. |
| `string` | String | |
| `bool` | Boolean | |
| `null` | null | |
| `map` | Object | JSON Objects become Maps (generic parsing) or populate Map/Struct targets. |
| `struct` | Object | Serializes public fields. Unmarshals matching keys to fields. |
| `array` | Array | |

## Special Handling

- **Missing Fields:** When populating a Struct via `json_unmarshal`, fields present in the Struct but missing in the JSON are left untouched (preserving their original values).
- **Extra Fields:** Fields in the JSON that do not match any Struct field are ignored.
- **Type Mismatch:** The system attempts to cast compatible types (e.g., float to int if lossless), but fundamentally incompatible types (e.g., string to int) will generally result in the field being skipped or zero-valued.
- **Nested Structures:** `json_unmarshal` works recursively. If a Struct contains fields that are other Structs, the corresponding nested JSON object will populate that child Struct in-place.

## Strict observable encoding

The json module exposes strict encoding when callers need explicit failure:

```noxy
use json

let value: map[string, any] = {"name": "Noxy", "active": true}
let result: any = json.dumps_result(value)   // errors.Result<string> (use `use json select *` for the typed form)
if result.ok then
    print(result.value)
else
    print(result.failure.message)
end
```

With `use json select *` the wrapper is typed — `let r = dumps_result(value)`
is a `Result<string>`, `if r.ok then` narrows `r.value` to `string`, and
`try dumps_result(value)` propagates the failure from a function that returns
a `Result` (spec §7).

json.dumps_result accepts null, bool, int, finite float, string, arrays, and
recursively string-keyed maps. It rejects bytes, structs, references,
callables, channels, wait groups, non-string map keys, NaN, infinities,
unsupported runtime values, and cycles. Reusing an acyclic child in multiple
branches is valid.

On success, data contains complete JSON and error is empty. On failure, data is
empty and error is non-empty. The legacy json_dumps function is unchanged and
retains its permissive compatibility behavior.
