# JSON Support in Noxy

Noxy provides native support for high-performance JSON serialization and deserialization directly in the VM. This functionality allows seamless integration with REST APIs and configuration files.

## Native Functions

The core functionality is provided by two global native functions: `json_dumps` and `json_loads`.

### 1. Serialization: `json_dumps`

Converts a Noxy value into a JSON string.

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

### 2. Deserialization: `json_loads`

Parses a JSON string into Noxy values. It supports two modes: **Generic Parsing** and **Targeted Population**.

**Signature:**
```noxy
func json_loads(json_str: string, target?: any) -> any
```

#### Mode A: Generic Parsing (No Target)

If only the JSON string is provided, `json_loads` returns a generic structure using Maps and Arrays.

```noxy
let json: string = "{\"id\": 1, \"tags\": [\"a\", \"b\"]}"
let data: any = json_loads(json)

// data is a Map: { "id": 1, "tags": ["a", "b"] }
```

#### Mode B: Targeted Population (Deep Unmarshal)

If a second argument is provided (typically a struct instance passed by reference or directly), `json_loads` attempts to populate that object **in-place**. This allows type-safe unmarshalling into complex Structs.

**Arguments:**
1. `json_str`: The JSON string.
2. `target`: The variable to populate. Must be an object (Struct, Map, Array) or a reference to one.

**Returns:** 
- `true` (bool) if population succeeded.
- `false` (bool) if failed.

**Example:**
```noxy
struct Config
    port: int
    debug: bool
end

let conf: Config = Config(0, false) // Initialize empty
let json: string = "{\"port\": 8080, \"debug\": true}"

// Populate 'conf' in-place
json_loads(json, ref conf) // OR just 'conf' since objects are Ref types

print(conf.port) // 8080
```

## Type Mapping

| Noxy Type | JSON Type | Notes |
| :--- | :--- | :--- |
| `int`, `float` | Number | |
| `string` | String | |
| `bool` | Boolean | |
| `null` | null | |
| `map` | Object | |
| `struct` | Object | Serializes public fields. |
| `array` | Array | |

## Special Handling

- **Missing Fields:** When populating a Struct, fields present in the Struct but missing in the JSON are left untouched (preserving their original values).
- **Extra Fields:** Fields in the JSON that do not match any Struct field are ignored.
- **Type Mismatch:** The system attempts to cast compatible types (e.g., float to int if lossless), but fundamentally incompatible types may result in runtime warnings or zero-values depending on the strictness content.
- **Nested Structures:** Targeted population works recursively. If a Struct contains another Struct, the nested JSON object will populate the child Struct.
