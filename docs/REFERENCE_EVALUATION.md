# Noxy Reference Consistency Evaluation

## 1. Summary
The evaluation reveals a **significant inconsistency** in how references (`ref T`) behave depending on their storage scope (Local Variable vs. Struct Field / Global). Additionally, the lack of an explicit dereference operator creates usability gaps, and **missing type propagation** for struct fields breaks reference usage in expressions.

## 2. Inconsistencies Detected

### A. Local Variables vs (Fields & Globals)
There are two distinct behaviors for `ref` types:

| Feature | Local Reference (`let r: ref int`) | Field/Global Reference (`struct { r: ref int }`) |
|---------|------------------------------------|--------------------------------------------------|
| **Assignment (`lhs = val`)** | **Implicit Dereference Assignment**.<br>`r = 5` updates the *value* pointed to by `r`. | **Rebind / Pointer Assignment**.<br>`s.r = ref y` updates the *pointer* `s.r`.<br>`s.r = 5` is a Type Mismatch error. |
| **Rebinding** | **Impossible**.<br>You cannot change what a local ref points to after declaration. | **Allowed**.<br>You can freely change the pointer to target another object. |
| **Expressions (`lhs + 1`)** | **Works**. Compiler knows `r` is `ref int` and emits implicit `deref`. | **Fails**. Compiler does not track field types, so it fails to emit `deref`. yields Runtime Error. |

### B. The "Update Problem" for Fields
Because Struct Fields behave like pointers but the language lacks an explicit dereference operator (e.g., `*ptr`), and auto-dereference logic is flawed for properties:
1. It is **impossible** to directly update the value pointed to by a struct field reference (`s.x = 5` fails).
2. It is **impossible** to use the value in expressions (`s.x + 1` crashes at runtime).

**Example:**
```noxy
struct Container
    val: ref int
end
let c: Container = Container(ref x)

// 1. Update Fail
c.val = 20      // ERROR: Type Mismatch (Compiler thinks assignment to pointer)

// 2. Expression Fail
let y: int = c.val + 1 // RUNTIME ERROR: "operands must be numbers" (VM sees Ref + Int)
```

## 3. Root Causes
1. **Compiler AssignStmt Logic**: Differentiates between `Identifier` targets (supports implicit store-via-ref for locals) and `MemberAccess` targets (supports only property set).
2. **Missing Field Type Resolution**: The compiler's `MemberAccessExpression` visitor does not resolve the field's type from the struct definition. It returns `nil` (unknown type). Consequently, `InfixExpression` logic (which checks for `RefType` to emit `OP_DEREF`) is skipped.

## 4. Consistency Recommendation
To harmonize the language:

1. **Implement Field Type Resolution**: The compiler MUST look up struct definitions to know that `c.val` is `ref int`. This will fix the expression crash.
2. **Unify Assignment Semantics**:
   - **Option A (Reference Model)**: Make `field = val` perform implicit dereference assignment if the field is a `ref`. This aligns with Locals.
   - **Option B (Pointer Model)**: Remove implicit dereference for locals. Require explicit dereference syntax (`*r = val`) for both.

Currently, `ref` in Noxy is a hybrid that is difficult to use correctly outside of simple local parameter passing.
