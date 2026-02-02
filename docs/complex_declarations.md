# Complex Declarations in Noxy

Noxy supports complex, nested data structures for Lists and Maps. This document illustrates standard patterns for declaring and initializing these structures.

## 1. Nested Lists (Multi-dimensional Arrays)

You can create lists of lists (matrices) using the `Type[][]` syntax.

```noxy
// Declaration and Initialization
let matrix: int[][] = [
    [1, 2, 3], 
    [4, 5, 6], 
    [7, 8, 9]
]

// Access
print(matrix[1][2]) // Outputs: 6
```

## 2. Nested Maps

Maps can store other maps as values using the `map[Key, Value]` syntax recursively.

```noxy
// Declaration: A map where keys are strings and values are maps of string->int
let user_scores: map[string, map[string, int]] = {
    "Alice": {"Math": 95, "History": 88},
    "Bob":   {"Math": 70, "History": 92}
}

// Access
print(user_scores["Alice"]["Math"]) // Outputs: 95
```

## 3. List of Maps

A list where each element is a map. Useful for storing records.

```noxy
// Declaration
let users: map[string, string][] = [
    {"id": "1", "name": "Alice"},
    {"id": "2", "name": "Bob"}
]

// Access
print(users[0]["name"]) // Outputs: Alice
```

## 4. Map of Lists

A map where the values are lists. Useful for grouping data.

```noxy
// Declaration
let groups: map[string, string[]] = {
    "admins": ["alice", "charlie"],
    "editors": ["bob"]
}

// Access
print(groups["admins"][1]) // Outputs: charlie
```

## 5. Structs with Complex Fields

Structs can contain arbitrarily nested types.

```noxy
struct Grid
    name: string
    points: int[][]
    metadata: map[string, string]
end

func main()
    let g: Grid = Grid(
        "Terrain", 
        [[0, 1], [1, 0]], 
        {"author": "Noxy"}
    )
    
    print(g.points[0][1]) // Outputs: 1
end
```
