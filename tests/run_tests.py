#!/usr/bin/env python3
"""
Script para executar testes graduais do compilador Noxy
Identifica onde e como os erros acontecem para facilitar a correção
"""

import subprocess
import os
import sys

def run_test(test_file):
    """Executa um teste específico e retorna o resultado"""
    print(f"\n{'='*60}")
    print(f"EXECUTANDO: {test_file}")
    print('='*60)
    
    try:
        # Tentar compilar
        compile_cmd = [
            "uv", "run", "python", "compiler.py", 
            "--compile", f"tests/{test_file}"
        ]
        
        result = subprocess.run(
            compile_cmd, 
            capture_output=True, 
            text=True, 
            cwd="."
        )
        
        if result.returncode == 0:
            print("✅ COMPILAÇÃO: SUCESSO")
            
            # Tentar executar
            try:
                # Compilar para executável (incluindo casting_functions.c)
                gcc_cmd = ["gcc", "-mcmodel=large", "casting_functions.c", "output.obj", "-o", "test_program.exe"]
                gcc_result = subprocess.run(gcc_cmd, capture_output=True, text=True)
                
                if gcc_result.returncode == 0:
                    # Executar o programa
                    exec_result = subprocess.run(["./test_program.exe"], capture_output=True, text=True)
                    print("✅ EXECUÇÃO: SUCESSO")
                    print("\nSAÍDA:")
                    print(exec_result.stdout)
                    return "PASS"
                else:
                    print("❌ ERRO NO GCC:")
                    print(gcc_result.stderr)
                    return "GCC_ERROR"
            except Exception as e:
                print(f"❌ ERRO NA EXECUÇÃO: {e}")
                return "EXEC_ERROR"
        else:
            print("❌ ERRO DE COMPILAÇÃO:")
            print(result.stdout)
            print(result.stderr)
            return "COMPILE_ERROR"
            
    except Exception as e:
        print(f"❌ ERRO GERAL: {e}")
        return "GENERAL_ERROR"

def main():
    """Executa todos os testes em ordem"""
    tests = [
        "test01_struct_basic.nx",
        "test02_struct_with_string.nx", 
        "test03_struct_nested.nx",
        "test04_array_simple.nx",
        "test05_array_complex.nx",
        "test06_struct_in_struct.nx",
        "test07_pair_struct.nx",
        "test08_array_of_pairs.nx",
        "test09_global_struct.nx",
        "test10_function_return_struct.nx"
    ]
    
    results = {}
    
    print("INICIANDO BATERIA DE TESTES DO COMPILADOR NOXY")
    print("Objetivo: Identificar onde e como os erros acontecem")
    
    for test in tests:
        if not os.path.exists(f"tests/{test}"):
            print(f"\n❌ ARQUIVO NÃO ENCONTRADO: {test}")
            results[test] = "NOT_FOUND"
            continue
            
        result = run_test(test)
        results[test] = result
        
        # Se o teste falhou, paramos aqui para análise
        if result != "PASS":
            print(f"\n🛑 TESTE FALHOU: {test}")
            print("Pare aqui para analisar o problema antes de continuar.")
            break
    
    # Resumo final
    print(f"\n{'='*60}")
    print("RESUMO DOS TESTES")
    print('='*60)
    
    for test, result in results.items():
        status_icon = "✅" if result == "PASS" else "❌"
        print(f"{status_icon} {test}: {result}")
    
    # Identificar o primeiro erro
    failed_tests = [test for test, result in results.items() if result != "PASS"]
    if failed_tests:
        print(f"\n🎯 PRIMEIRO ERRO: {failed_tests[0]}")
        print("Este é o teste que precisa ser corrigido primeiro no compilador.")
    else:
        print("\n🎉 TODOS OS TESTES PASSARAM!")

if __name__ == "__main__":
    main()
