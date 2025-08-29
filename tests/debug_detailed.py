#!/usr/bin/env python3

import sys
import os
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

from compiler import *

# Override the _generate_expression method to add debug info
original_generate_expression = LLVMCodeGenerator._generate_expression

def debug_generate_expression(self, node, expected_type=None):
    if isinstance(node, ArrayNode) and isinstance(node.element_type, StructType):
        print(f"DEBUG: ArrayNode with StructType elements")
        print(f"  element_type: {node.element_type}")
        print(f"  expected_type: {expected_type}")
        print(f"  num_elements: {len(node.elements)}")
        
    result = original_generate_expression(self, node, expected_type)
    
    if isinstance(node, ArrayNode) and isinstance(node.element_type, StructType):
        print(f"  result type: {result.type}")
        print(f"  result: {result}")
        
    return result

LLVMCodeGenerator._generate_expression = debug_generate_expression

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

try:
    lexer = Lexer(code)
    tokens = lexer.tokenize()
    parser = Parser(tokens)
    ast = parser.parse()
    
    generator = LLVMCodeGenerator()
    generator.generate(ast)
    print("SUCCESS: Compilation completed!")
except Exception as e:
    print(f"ERROR: {e}")
    import traceback
    traceback.print_exc()
