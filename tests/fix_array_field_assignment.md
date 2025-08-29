# Correção para Array Field Assignment

## Problema Identificado

O parser Noxy atual não reconhece `array[index].field = value` como um lvalue válido para atribuição.

### Localização do Problema

**Arquivo:** `compiler.py`
**Linha:** ~931-944 na função `_parse_statement()`

### Código Atual (Problemático)

```python
elif next_token.type == TokenType.LBRACKET:
    # Pode ser acesso de array seguido de atribuição
    saved_pos = self.position
    identifier = self._advance()
    self._advance()  # [
    index_expr = self._parse_expression()
    if self._match(TokenType.RBRACKET) and self._check(TokenType.ASSIGN):
        # É uma atribuição de array
        self.position = saved_pos
        return self._parse_array_assignment()
    else:
        # É apenas um acesso de array, voltar e processar como expressão
        self.position = saved_pos
        return self._parse_expression()
```

### Problema

O código acima só verifica se após `array[index]` vem diretamente `=`, mas não verifica se pode vir `.field =`.

### Solução Necessária

Precisamos modificar a lógica para também detectar o padrão `array[index].field = value`.

#### Casos que devem ser suportados:
1. `array[index] = value` ✅ (já funciona)
2. `array[index].field = value` ❌ (precisa ser implementado)
3. `array[index].field.subfield = value` ❌ (precisa ser implementado)

### Estratégia de Correção

1. **Ampliar a verificação de lookahead**: Após detectar `array[index]`, verificar se vem `.field` antes do `=`
2. **Criar novo tipo de AST node**: `ArrayFieldAssignmentNode` para representar `array[index].field = value`
3. **Implementar parser**: Função `_parse_array_field_assignment()`
4. **Implementar geração de código**: Suporte a `ArrayFieldAssignmentNode` no LLVM code generator

### Testes Necessários Após Correção

```noxy
// Caso simples
struct Point
    x: int
end

let points: Point[2] = [Point(10), Point(20)]
points[0].x = 50  // ← Este deve funcionar após a correção

// Caso mais complexo
struct Person
    name: string,
    age: int
end

let people: Person[3] = [Person("João", 30), Person("Maria", 25), Person("Carlos", 35)]
people[1].name = "Ana"
people[2].age = 40
```

### Próximos Passos

1. Implementar a correção no parser
2. Criar o novo AST node
3. Implementar a geração de código LLVM
4. Testar com os casos de teste
5. Verificar se funciona com structs aninhados
