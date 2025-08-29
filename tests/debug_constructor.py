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
