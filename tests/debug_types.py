#!/usr/bin/env python3

import sys
import os
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

from compiler import *

# Parse do código de teste
code = """
struct Point
    x: int
end

struct Container
    points: Point[1]
end

let container: Container = Container([Point(10)])
"""

# Tokenizar
lexer = Lexer(code)
tokens = lexer.tokenize()

# Parse
parser = Parser(tokens)
ast = parser.parse()

# Geração de código  
generator = LLVMCodeGenerator()

# Processar structs
for node in ast.statements:
    if isinstance(node, StructDefinitionNode):
        print(f"Processing struct: {node.name}")
        for field_name, field_type in node.fields:
            print(f"  Field {field_name}: {field_type}")
            llvm_type = generator._convert_type(field_type)
            print(f"    LLVM Type: {llvm_type}")
        generator._process_struct_definition(node)
        print(f"    Final LLVM Struct: {generator.struct_types[node.name]}")
        print()

print("Struct definitions processed successfully!")
