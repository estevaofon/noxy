# Noxy Compiler
# Compilador para a linguagem Noxy com tipagem estática

import re
import sys
import os
import importlib.util
from enum import Enum
from dataclasses import dataclass, field
from typing import List, Optional, Union, Dict, Tuple, Any
import llvmlite.ir as ir
import llvmlite.binding as llvm

# Classes de erro personalizadas para melhor diagnóstico
class NoxyError(Exception):
    """Classe base para todos os erros do compilador Noxy"""
    def __init__(self, message: str, line: int = None, column: int = None, source_line: str = None):
        self.message = message
        self.line = line
        self.column = column
        self.source_line = source_line
        super().__init__(self.format_error())
    
    def format_error(self) -> str:
        """Formata o erro com informações de contexto"""
        if self.line is not None and self.column is not None:
            error_msg = f"Erro na linha {self.line}, coluna {self.column}: {self.message}"
            if self.source_line:
                error_msg += f"\n  {self.source_line}"
                if self.column > 0:
                    error_msg += f"\n  {' ' * (self.column - 1)}^"
            return error_msg
        return f"Erro: {self.message}"

class NoxySyntaxError(NoxyError):
    """Erro de sintaxe na análise do código"""
    pass

class NoxySemanticError(NoxyError):
    """Erro semântico (tipos, variáveis não declaradas, etc.)"""
    pass

class NoxyCodeGenError(NoxyError):
    """Erro na geração de código LLVM"""
    pass

class NoxyRuntimeError(NoxyError):
    """Erro de tempo de execução"""
    pass

# Tipos de tokens
class TokenType(Enum):
    # Literais
    NUMBER = "NUMBER"
    FLOAT = "FLOAT"
    STRING = "STRING"
    FSTRING = "FSTRING"
    IDENTIFIER = "IDENTIFIER"
    TRUE = "TRUE"
    FALSE = "FALSE"
    NULL = "NULL"
    
    # Palavras-chave
    LET = "LET"
    GLOBAL = "GLOBAL"
    IF = "IF"
    THEN = "THEN"
    ELSE = "ELSE"
    END = "END"
    WHILE = "WHILE"
    DO = "DO"
    PRINT = "PRINT"
    FUNC = "FUNC"
    RETURN = "RETURN"
    STRUCT = "STRUCT"
    REF = "REF"  # Nova palavra-chave para referências
    BREAK = "BREAK"  # Nova palavra-chave para interromper loops
    USE = "USE"  # Nova palavra-chave para importar módulos
    SELECT = "SELECT"  # Nova palavra-chave para selecionar símbolos específicos
    
    # Tipos
    INT = "INT"
    FLOAT_TYPE = "FLOAT_TYPE"
    STRING_TYPE = "STRING_TYPE"
    STR_TYPE = "STR_TYPE"
    VOID = "VOID"
    BOOL = "BOOL"
    
    # Operadores
    PLUS = "PLUS"
    MINUS = "MINUS"
    MULTIPLY = "MULTIPLY"
    DIVIDE = "DIVIDE"
    MODULO = "MODULO"  # Adicionado
    ASSIGN = "ASSIGN"
    GT = "GT"
    LT = "LT"
    GTE = "GTE"
    LTE = "LTE"
    EQ = "EQ"
    NEQ = "NEQ"
    ARROW = "ARROW"
    CONCAT = "CONCAT"
    AND = "AND"
    OR = "OR"
    NOT = "NOT"
    
    # Delimitadores
    LPAREN = "LPAREN"
    RPAREN = "RPAREN"
    LBRACKET = "LBRACKET"
    RBRACKET = "RBRACKET"
    LBRACE = "LBRACE"
    RBRACE = "RBRACE"
    COMMA = "COMMA"
    COLON = "COLON"
    SEMICOLON = "SEMICOLON"
    DOT = "DOT"
    
    # Especiais
    ZEROS = "ZEROS"
    
    # Fim de arquivo
    EOF = "EOF"

@dataclass
class Token:
    type: TokenType
    value: Union[str, int, float]
    line: int
    column: int

# Sistema de tipos
@dataclass
class Type:
    pass

@dataclass
class IntType(Type):
    pass

@dataclass 
class FloatType(Type):
    pass

@dataclass
class StringType(Type):
    pass

@dataclass
class StrType(Type):  # Alias para StringType
    pass

@dataclass
class ArrayType(Type):
    element_type: Type
    size: Optional[int] = None

@dataclass
class VoidType(Type):
    pass

@dataclass
class FunctionType(Type):
    param_types: List[Type]
    return_type: Type

@dataclass
class BoolType(Type):
    pass

@dataclass
class NullType(Type):
    pass

@dataclass
class StructType(Type):
    name: str
    fields: Dict[str, Type]

@dataclass
class ReferenceType(Type):
    """Tipo para referências que permitem auto-referenciamento"""
    target_type: Type
    is_mutable: bool = True

# Lexer
class Lexer:
    def __init__(self, source: str):
        self.source = source
        self.source_lines = source.split('\n')  # Manter linhas para contexto de erro
        self.position = 0
        self.line = 1
        self.column = 1
        self.tokens = []
        
    def tokenize(self) -> List[Token]:
        while self.position < len(self.source):
            self._skip_whitespace_and_comments()
            
            if self.position >= len(self.source):
                break
                
            # F-strings e strings
            if self._current_char() == 'f' and self._peek_char() == '"':
                self._read_fstring()
            elif self._current_char() == '"':
                self._read_string()
            # Números
            elif self._current_char().isdigit():
                self._read_number()
            # Identificadores e palavras-chave
            elif self._current_char().isalpha() or self._current_char() == '_':
                self._read_identifier()
            # Operadores e delimitadores
            else:
                self._read_operator()
                
        self.tokens.append(Token(TokenType.EOF, "", self.line, self.column))
        return self.tokens
    
    def _current_char(self) -> str:
        if self.position < len(self.source):
            return self.source[self.position]
        return ""
    
    def _peek_char(self) -> str:
        if self.position + 1 < len(self.source):
            return self.source[self.position + 1]
        return ""
    
    def _advance(self):
        if self.position < len(self.source) and self.source[self.position] == '\n':
            self.line += 1
            self.column = 1
        else:
            self.column += 1
        self.position += 1
    
    def _skip_whitespace_and_comments(self):
        while self.position < len(self.source):
            # Pular espaços em branco
            if self.source[self.position].isspace():
                self._advance()
            # Pular comentários de linha
            elif self.position + 1 < len(self.source) and self.source[self.position:self.position+2] == '//':
                while self.position < len(self.source) and self.source[self.position] != '\n':
                    self._advance()
            else:
                break
    
    def _read_string(self):
        start_column = self.column
        self._advance()  # Pular aspas inicial
        string_value = ""
        
        while self.position < len(self.source) and self._current_char() != '"':
            if self._current_char() == '\\':
                self._advance()
                if self.position < len(self.source):
                    escape_char = self._current_char()
                    if escape_char == 'n':
                        string_value += '\n'
                    elif escape_char == 't':
                        string_value += '\t'
                    elif escape_char == '"':
                        string_value += '"'
                    elif escape_char == '\\':
                        string_value += '\\'
                    elif escape_char == '0':
                        string_value += '\0'
                    else:
                        string_value += escape_char
                    self._advance()
            else:
                string_value += self._current_char()
                self._advance()
        
        if self.position < len(self.source):
            self._advance()  # Pular aspas final
        else:
            source_line = self.source_lines[self.line - 1] if self.line <= len(self.source_lines) else ""
            raise NoxySyntaxError(
                "String não terminada - esperado '\"'",
                self.line, self.column, source_line
            )
            
        self.tokens.append(Token(TokenType.STRING, string_value, self.line, start_column))
    
    def _read_fstring(self):
        start_column = self.column
        self._advance()  # Pular 'f'
        self._advance()  # Pular aspas inicial
        
        parts = []
        current_text = ""
        
        while self.position < len(self.source) and self._current_char() != '"':
            if self._current_char() == '{':
                # Se há texto acumulado, adicionar como string literal
                if current_text:
                    parts.append(current_text)
                    current_text = ""
                
                # Ler expressão dentro das chaves
                self._advance()  # Pular '{'
                expr_text = ""
                brace_count = 1
                
                while self.position < len(self.source) and brace_count > 0:
                    if self._current_char() == '{':
                        brace_count += 1
                    elif self._current_char() == '}':
                        brace_count -= 1
                    
                    if brace_count > 0:
                        expr_text += self._current_char()
                    
                    self._advance()
                
                if brace_count > 0:
                    source_line = self.source_lines[self.line - 1] if self.line <= len(self.source_lines) else ""
                    raise NoxySyntaxError(
                        "F-string não terminada - esperado '}'",
                        self.line, self.column, source_line
                    )
                
                # Verificar se há especificador de formato (ex: variable:.2f)
                expr_content = expr_text.strip()
                format_spec = None
                
                if ':' in expr_content:
                    expr_part, format_part = expr_content.split(':', 1)
                    expr_content = expr_part.strip()
                    format_spec = format_part.strip()
                
                # Adicionar expressão como token especial
                if format_spec:
                    parts.append(('EXPR_FORMAT', expr_content, format_spec))
                else:
                    parts.append(('EXPR', expr_content))
                
            elif self._current_char() == '\\':
                # Processar escape sequences
                self._advance()
                if self.position < len(self.source):
                    escape_char = self._current_char()
                    if escape_char == 'n':
                        current_text += '\n'
                    elif escape_char == 't':
                        current_text += '\t'
                    elif escape_char == '"':
                        current_text += '"'
                    elif escape_char == '\\':
                        current_text += '\\'
                    elif escape_char == '0':
                        current_text += '\0'
                    else:
                        current_text += escape_char
                    self._advance()
            else:
                current_text += self._current_char()
                self._advance()
        
        # Adicionar texto restante se houver
        if current_text:
            parts.append(current_text)
        
        if self.position < len(self.source):
            self._advance()  # Pular aspas final
        else:
            source_line = self.source_lines[self.line - 1] if self.line <= len(self.source_lines) else ""
            raise NoxySyntaxError(
                "F-string não terminada - esperado '\"'",
                self.line, self.column, source_line
            )
        
        self.tokens.append(Token(TokenType.FSTRING, parts, self.line, start_column))
    
    def _read_number(self):
        start_column = self.column
        number_str = ""
        has_dot = False
        
        while self.position < len(self.source) and (self._current_char().isdigit() or self._current_char() == '.'):
            if self._current_char() == '.':
                if has_dot:
                    break  # Segunda vírgula, parar
                has_dot = True
            number_str += self._current_char()
            self._advance()
            
        if has_dot:
            value = float(number_str)
            self.tokens.append(Token(TokenType.FLOAT, value, self.line, start_column))
        else:
            value = int(number_str)
            self.tokens.append(Token(TokenType.NUMBER, value, self.line, start_column))
    
    def _read_identifier(self):
        start_column = self.column
        identifier = ""
        
        while self.position < len(self.source) and (self._current_char().isalnum() or self._current_char() == '_'):
            identifier += self._current_char()
            self._advance()
            
        # Verificar palavras-chave
        keywords = {
            'let': TokenType.LET,
            'global': TokenType.GLOBAL,
            'if': TokenType.IF,
            'then': TokenType.THEN,
            'else': TokenType.ELSE,
            'end': TokenType.END,
            'while': TokenType.WHILE,
            'do': TokenType.DO,
            'print': TokenType.PRINT,
            'func': TokenType.FUNC,
            'return': TokenType.RETURN,
            'int': TokenType.INT,
            'float': TokenType.FLOAT_TYPE,
            'string': TokenType.STRING_TYPE,
            'str': TokenType.STR_TYPE,
            'void': TokenType.VOID,
            'bool': TokenType.BOOL,
            'true': TokenType.TRUE,
            'false': TokenType.FALSE,
            'null': TokenType.NULL,
            'struct': TokenType.STRUCT,
            'ref': TokenType.REF,
            'zeros': TokenType.ZEROS,
            'break': TokenType.BREAK,
            'use': TokenType.USE,
            'select': TokenType.SELECT
        }
        
        token_type = keywords.get(identifier, TokenType.IDENTIFIER)
        self.tokens.append(Token(token_type, identifier, self.line, start_column))
    
    def _read_operator(self):
        start_column = self.column
        char = self._current_char()
        next_char = self._peek_char()
        
        # Operadores de dois caracteres
        two_char_ops = {
            '>=': TokenType.GTE,
            '<=': TokenType.LTE,
            '==': TokenType.EQ,
            '!=': TokenType.NEQ,
            '->': TokenType.ARROW,
            '++': TokenType.CONCAT
        }
        
        two_char = char + next_char
        if two_char in two_char_ops:
            self.tokens.append(Token(two_char_ops[two_char], two_char, self.line, start_column))
            self._advance()
            self._advance()
            return
        
        # Operadores de um caractere
        operators = {
            '+': TokenType.PLUS,
            '-': TokenType.MINUS,
            '*': TokenType.MULTIPLY,
            '/': TokenType.DIVIDE,
            '%': TokenType.MODULO,  # Adicionado
            '=': TokenType.ASSIGN,
            '>': TokenType.GT,
            '<': TokenType.LT,
            '(': TokenType.LPAREN,
            ')': TokenType.RPAREN,
            '[': TokenType.LBRACKET,
            ']': TokenType.RBRACKET,
            '{': TokenType.LBRACE,
            '}': TokenType.RBRACE,
            ',': TokenType.COMMA,
            ':': TokenType.COLON,
            ';': TokenType.SEMICOLON,
            '.': TokenType.DOT,
            '&': TokenType.AND,
            '|': TokenType.OR,
            '!': TokenType.NOT
        }
        
        if char in operators:
            self.tokens.append(Token(operators[char], char, self.line, start_column))
            self._advance()
        else:
            source_line = self.source_lines[self.line - 1] if self.line <= len(self.source_lines) else ""
            raise NoxySyntaxError(
                f"Caractere inválido '{char}'",
                self.line, self.column, source_line
            )

# AST (Abstract Syntax Tree)
class ASTNode:
    def __init__(self):
        self.line = None
        self.column = None
        self.source_line = None
        
    def set_location(self, token: 'Token', source_lines: List[str] = None):
        """Define localização do nó baseado em token"""
        self.line = token.line
        self.column = token.column
        if source_lines and token.line <= len(source_lines):
            self.source_line = source_lines[token.line - 1]
        return self
    
    def has_location(self) -> bool:
        """Verifica se o nó tem informações de localização"""
        return self.line is not None and self.column is not None

@dataclass
class NumberNode(ASTNode):
    value: int
    
    def __post_init__(self):
        super().__init__()

@dataclass
class FloatNode(ASTNode):
    value: float
    
    def __post_init__(self):
        super().__init__()

@dataclass
class StringNode(ASTNode):
    value: str

@dataclass
class FStringNode(ASTNode):
    parts: List[Union[str, ASTNode]]  # Mistura de strings literais e expressões
    
    def __post_init__(self):
        super().__init__()

@dataclass
class ArrayNode(ASTNode):
    elements: List[ASTNode]
    element_type: Type
    
    def __post_init__(self):
        super().__init__()

@dataclass
class ZerosNode(ASTNode):
    size: int
    element_type: Type
    
    def __post_init__(self):
        super().__init__()

@dataclass
class ArrayAccessNode(ASTNode):
    array_name: str
    index: ASTNode
    
    def __post_init__(self):
        super().__init__()

@dataclass
class IdentifierNode(ASTNode):
    name: str
    
    def __post_init__(self):
        super().__init__()

@dataclass
class BinaryOpNode(ASTNode):
    left: ASTNode
    operator: TokenType
    right: ASTNode

@dataclass
class CastNode(ASTNode):
    expression: ASTNode
    target_type: Type

@dataclass
class ConcatNode(ASTNode):
    left: ASTNode
    right: ASTNode

@dataclass
class AssignmentNode(ASTNode):
    identifier: str
    var_type: Type
    value: ASTNode
    is_global: bool = False

@dataclass
class ArrayAssignmentNode(ASTNode):
    array_name: str
    index: ASTNode
    value: ASTNode

@dataclass
class PrintNode(ASTNode):
    expression: ASTNode

@dataclass
class IfNode(ASTNode):
    condition: ASTNode
    then_branch: List[ASTNode]
    else_branch: Optional[List[ASTNode]] = None

@dataclass
class WhileNode(ASTNode):
    condition: ASTNode
    body: List[ASTNode]

@dataclass
class FunctionNode(ASTNode):
    name: str
    params: List[Tuple[str, Type]]
    return_type: Type
    body: List[ASTNode]

@dataclass
class ReturnNode(ASTNode):
    value: Optional[ASTNode]

@dataclass
class CallNode(ASTNode):
    function_name: str
    arguments: List[ASTNode]
    
    def __post_init__(self):
        super().__init__()

@dataclass
class StructDefinitionNode(ASTNode):
    name: str
    fields: List[Tuple[str, Type]]

@dataclass
class StructAccessNode(ASTNode):
    struct_name: str
    field_name: str
    
    def __post_init__(self):
        super().__init__()

@dataclass
class StructAccessFromArrayNode(ASTNode):
    """Acesso a campo de um elemento de array: arr[idx].campo (suporta aninhado)"""
    base_access: ArrayAccessNode
    field_path: str

@dataclass
class StructAssignmentNode(ASTNode):
    struct_name: str
    field_name: str
    value: ASTNode

@dataclass
class NestedStructAssignmentNode(ASTNode):
    """Nó para atribuições aninhadas de struct: struct.campo.subcampo = valor"""
    struct_name: str
    field_path: List[str]  # Lista de campos para navegar: ["endereco", "rua"]
    value: ASTNode

@dataclass
class ArrayFieldAssignmentNode(ASTNode):
    """Nó para atribuições de campo de struct em array: array[index].campo = valor"""
    array_name: str
    index: ASTNode
    field_path: List[str]  # Lista de campos para navegar: ["campo"] ou ["campo", "subcampo"]
    value: ASTNode

@dataclass
class StructConstructorNode(ASTNode):
    """Nó para construtores de struct: StructName(arg1, arg2, ...)"""
    struct_name: str
    arguments: List[ASTNode]

@dataclass
class ProgramNode(ASTNode):
    statements: List[ASTNode]

@dataclass
class BooleanNode(ASTNode):
    value: bool

@dataclass
class NullNode(ASTNode):
    pass

@dataclass
class UnaryOpNode(ASTNode):
    operator: TokenType
    operand: ASTNode

@dataclass
class ReferenceNode(ASTNode):
    """Nó para referências: ref expressao"""
    expression: ASTNode

@dataclass
class BreakNode(ASTNode):
    """Nó para a keyword break"""
    
    def __post_init__(self):
        super().__init__()

@dataclass
class StringCharAccessNode(ASTNode):
    string: str
    index: ASTNode

@dataclass
class UseNode(ASTNode):
    """
    Representa uma instrução de importação.
    module_name: nome do módulo a ser importado
    selected_symbols: lista de símbolos específicos ou None para importar tudo
    import_all: True se usar 'use module select *'
    """
    module_name: str
    selected_symbols: Optional[List[str]] = None
    import_all: bool = False

# Parser
class ErrorContext:
    """Context manager para capturar e propagar erros com localização"""
    def __init__(self, parser: 'Parser', current_node: ASTNode = None):
        self.parser = parser
        self.current_node = current_node
        
    def __enter__(self):
        return self
        
    def __exit__(self, exc_type, exc_val, exc_tb):
        if exc_type and issubclass(exc_type, Exception) and not issubclass(exc_type, (NoxyError,)):
            # Capturar exceções genéricas e convertê-las em erros com localização
            if self.current_node and self.current_node.has_location():
                # Usar localização do nó atual
                source_line = self.current_node.source_line or (
                    self.parser.source_lines[self.current_node.line - 1] 
                    if self.current_node.line <= len(self.parser.source_lines)
                    else ""
                )
                raise NoxySemanticError(str(exc_val), self.current_node.line, self.current_node.column, source_line) from exc_val
            else:
                # Usar token atual do parser
                token = self.parser._current_token()
                source_line = self.parser._get_source_line(token.line)
                raise NoxySemanticError(str(exc_val), token.line, token.column, source_line) from exc_val
        return False

class Parser:
    def __init__(self, tokens: List[Token], source_lines: List[str] = None):
        self.tokens = tokens
        self.source_lines = source_lines or []  # Linhas de código fonte para contexto
        self.position = 0
        self.struct_types = {}  # Armazenar tipos de struct definidos
        self.defined_functions = set()  # Conjunto de funções definidas
        self.defined_structs = set()    # Conjunto de structs definidos
        # Profundidade de funções para identificar escopo atual (0 = topo do arquivo)
        self.in_function_depth = 0
        self._current_context_node = None  # Para rastreamento de contexto
    
    def _get_source_line(self, line_num: int) -> str:
        """Obtém a linha de código fonte para contexto de erro"""
        if 1 <= line_num <= len(self.source_lines):
            return self.source_lines[line_num - 1]
        return ""
    
    def _error(self, message: str) -> None:
        """Lança um erro de sintaxe com informações de linha e coluna"""
        token = self._current_token()
        source_line = self._get_source_line(token.line)
        raise NoxySyntaxError(message, token.line, token.column, source_line)
    
    def _error_at_current(self, message: str) -> None:
        """Lança um erro de sintaxe para o token atual"""
        token = self._current_token()
        source_line = self._get_source_line(token.line)
        raise NoxySyntaxError(message, token.line, token.column, source_line)
    
    def _error_at_previous(self, message: str) -> None:
        """Lança um erro de sintaxe para o token anterior"""
        if self.position > 0:
            token = self.tokens[self.position - 1]
            source_line = self._get_source_line(token.line)
            raise NoxySyntaxError(message, token.line, token.column, source_line)
        else:
            raise NoxySyntaxError(f"Erro no início do arquivo: {message}")
    
    def _add_location_info(self, node: ASTNode, token: Token = None) -> ASTNode:
        """Adiciona informações de linha e coluna ao nó AST"""
        if token is None:
            token = self._current_token()
        return node.set_location(token, self.source_lines)
    
    def _create_node_with_location(self, node_class, *args, token: Token = None, **kwargs) -> ASTNode:
        """Cria um nó AST com localização automática"""
        if token is None:
            token = self._current_token()
        node = node_class(*args, **kwargs)
        return self._add_location_info(node, token)
    
    def _with_context(self, node: ASTNode = None):
        """Retorna context manager para captura de erros com localização"""
        return ErrorContext(self, node)
    
    def _semantic_error(self, message: str, node: ASTNode = None) -> None:
        """Lança um erro semântico com informações de linha e coluna"""
        if node and node.line is not None and node.column is not None:
            source_line = self._get_source_line(node.line)
            raise NoxySemanticError(message, node.line, node.column, source_line)
        else:
            raise NoxySemanticError(message)
        
    def parse(self) -> ProgramNode:
        statements = []
        
        while not self._is_at_end():
            stmt = self._parse_statement()
            if stmt:
                statements.append(stmt)
                
        return ProgramNode(statements)
    
    def _current_token(self) -> Token:
        return self.tokens[self.position]
    
    def _advance(self) -> Token:
        token = self.tokens[self.position]
        if not self._is_at_end():
            self.position += 1
        return token
    
    def _is_at_end(self) -> bool:
        return self._current_token().type == TokenType.EOF
    
    def _match(self, *types: TokenType) -> bool:
        for token_type in types:
            if self._current_token().type == token_type:
                self._advance()
                return True
        return False
    
    def _check(self, token_type: TokenType) -> bool:
        return self._current_token().type == token_type
    
    def _parse_type(self) -> Type:
        if self._match(TokenType.REF):
            # Tipo de referência: ref Tipo
            target_type = self._parse_type()
            return ReferenceType(target_type)
        elif self._match(TokenType.INT):
            if self._match(TokenType.LBRACKET):
                size = None
                if self._current_token().type == TokenType.NUMBER:
                    size = self._advance().value
                if not self._match(TokenType.RBRACKET):
                    self._error("Esperado ']' após tamanho do array")
                return ArrayType(IntType(), size)
            return IntType()
        elif self._match(TokenType.FLOAT_TYPE):
            if self._match(TokenType.LBRACKET):
                size = None
                if self._current_token().type == TokenType.NUMBER:
                    size = self._advance().value
                if not self._match(TokenType.RBRACKET):
                    self._error("Esperado ']' após tamanho do array")
                return ArrayType(FloatType(), size)
            return FloatType()
        elif self._match(TokenType.STRING_TYPE) or self._match(TokenType.STR_TYPE):
            if self._match(TokenType.LBRACKET):
                size = None
                if self._current_token().type == TokenType.NUMBER:
                    size = self._advance().value
                if not self._match(TokenType.RBRACKET):
                    self._error("Esperado ']' após tamanho do array")
                return ArrayType(StringType(), size)
            return StringType()
        elif self._match(TokenType.VOID):
            return VoidType()
        elif self._match(TokenType.BOOL):
            if self._match(TokenType.LBRACKET):
                size = None
                if self._current_token().type == TokenType.NUMBER:
                    size = self._advance().value
                if not self._match(TokenType.RBRACKET):
                    self._error("Esperado ']' após tamanho do array")
                return ArrayType(BoolType(), size)
            return BoolType()
        elif self._check(TokenType.IDENTIFIER):
            # Suporte a tipos de struct e arrays de struct (ex.: Pessoa[3])
            name_token = self._advance()  # Consumir o nome do tipo
            type_obj = self.struct_types.get(name_token.value, StructType(name_token.value, {}))
            # Verificar se é um array do tipo identificado
            if self._match(TokenType.LBRACKET):
                size = None
                if self._current_token().type == TokenType.NUMBER:
                    size = self._advance().value
                if not self._match(TokenType.RBRACKET):
                    self._error("Esperado ']' após tamanho do array")
                return ArrayType(type_obj, size)
            return type_obj
        else:
            self._error(f"Tipo esperado, encontrado {self._current_token()}")
    
    def _parse_statement(self) -> Optional[ASTNode]:
        if self._match(TokenType.LET):
            # No topo do arquivo, 'let' define variável global; dentro de função, local
            is_global = (getattr(self, 'in_function_depth', 0) == 0)
            return self._parse_assignment(is_global=is_global)
        elif self._match(TokenType.GLOBAL):
            # Manter suporte à keyword 'global' (opcional), sempre global
            return self._parse_assignment(is_global=True)
        elif self._match(TokenType.PRINT):
            return self._parse_print()
        elif self._match(TokenType.IF):
            return self._parse_if()
        elif self._match(TokenType.WHILE):
            return self._parse_while()
        elif self._match(TokenType.FUNC):
            return self._parse_function()
        elif self._match(TokenType.RETURN):
            return self._parse_return()
        elif self._match(TokenType.STRUCT):
            return self._parse_struct_definition()
        elif self._match(TokenType.BREAK):
            return self._parse_break()
        elif self._match(TokenType.USE):
            return self._parse_use()
        elif self._check(TokenType.IDENTIFIER):
            # Pode ser uma atribuição de array, reatribuição simples, acesso a struct ou chamada de função
            if self.position + 1 < len(self.tokens):
                next_token = self.tokens[self.position + 1]
                if next_token.type == TokenType.LBRACKET:
                    # Pode ser acesso de array seguido de atribuição
                    saved_pos = self.position
                    identifier = self._advance()
                    self._advance()  # [
                    index_expr = self._parse_expression()
                    if self._match(TokenType.RBRACKET):
                        if self._check(TokenType.ASSIGN):
                            # É uma atribuição de array simples: array[index] = value
                            self.position = saved_pos
                            return self._parse_array_assignment()
                        elif self._check(TokenType.DOT):
                            # Pode ser atribuição de campo de array: array[index].field = value
                            # Verificar se após o campo há um '='
                            temp_pos = self.position
                            self._advance()  # .
                            field_token = self._advance()
                            if field_token.type == TokenType.IDENTIFIER:
                                # Verificar se há mais campos (array[index].field.subfield)
                                while self._check(TokenType.DOT):
                                    self._advance()  # .
                                    next_field = self._advance()
                                    if next_field.type != TokenType.IDENTIFIER:
                                        break
                                
                                if self._check(TokenType.ASSIGN):
                                    # É uma atribuição de campo de array
                                    self.position = saved_pos
                                    return self._parse_array_field_assignment()
                            
                            # Não é atribuição, voltar e processar como expressão
                            self.position = saved_pos
                            return self._parse_expression()
                        else:
                            # É apenas um acesso de array, voltar e processar como expressão
                            self.position = saved_pos
                            return self._parse_expression()
                    else:
                        # Erro de sintaxe - esperado ']'
                        self.position = saved_pos
                        return self._parse_expression()
                elif next_token.type == TokenType.DOT:
                    # Pode ser acesso a campo de struct seguido de atribuição
                    saved_pos = self.position
                    struct_name = self._advance()
                    self._advance()  # .
                    field_name = self._advance()
                    if field_name.type != TokenType.IDENTIFIER:
                        self._error_at_previous("Esperado nome do campo após '.'")
                    
                    # Verificar se há acesso a array após o campo (ex: struct.campo[indice])
                    if self._check(TokenType.LBRACKET):
                        self._advance()  # [
                        index_expr = self._parse_expression()
                        if self._match(TokenType.RBRACKET) and self._check(TokenType.ASSIGN):
                            # É uma atribuição de array de campo de struct
                            self.position = saved_pos
                            return self._parse_array_assignment()
                        else:
                            # É apenas um acesso de array, voltar e processar como expressão
                            self.position = saved_pos
                            return self._parse_expression()
                    
                    # Verificar se há mais níveis de acesso (ex: pessoa.endereco.rua)
                    while self._check(TokenType.DOT):
                        self._advance()  # .
                        next_field = self._advance()
                        if next_field.type != TokenType.IDENTIFIER:
                            self._error_at_previous("Esperado nome do campo após '.'")
                    
                    if self._check(TokenType.ASSIGN):
                        # É uma atribuição de campo de struct
                        self.position = saved_pos
                        return self._parse_struct_assignment()
                    else:
                        # É apenas um acesso a campo, voltar e processar como expressão
                        self.position = saved_pos
                        return self._parse_expression()
                elif next_token.type == TokenType.ASSIGN:
                    # Reatribuição simples
                    return self._parse_reassignment()
                elif next_token.type == TokenType.LPAREN:
                    # Chamada de função
                    return self._parse_expression()
        
        return self._parse_expression()
    
    def _parse_reassignment(self) -> AssignmentNode:
        """Parse reatribuição de variável (sem let)"""
        identifier = self._advance()
        if identifier.type != TokenType.IDENTIFIER:
            self._error("Esperado identificador")
        
        if not self._match(TokenType.ASSIGN):
            self._error("Esperado '=' após identificador")
            
        value = self._parse_expression()
        
        # Reatribuição não tem tipo (será inferido da variável existente)
        return AssignmentNode(identifier.value, None, value, False)
    
    def _parse_assignment(self, is_global: bool = False) -> AssignmentNode:
        identifier = self._advance()
        if identifier.type != TokenType.IDENTIFIER:
            self._error("Esperado identificador após 'let' ou 'global'")
        
        var_type = None
        if self._match(TokenType.COLON):
            # Tipo explícito fornecido
            var_type = self._parse_type()
        
        if not self._match(TokenType.ASSIGN):
            self._error("Esperado '=' após tipo ou identificador")
            
        value = self._parse_expression()
        return AssignmentNode(identifier.value, var_type, value, is_global)
    
    def _parse_array_assignment(self) -> ArrayAssignmentNode:
        # Construir o nome do array (pode ser composto como "aluno.notas")
        array_name_parts = []
        
        # Primeiro identificador
        first_token = self._advance()
        if first_token.type != TokenType.IDENTIFIER:
            self._error("Esperado identificador para nome do array")
        array_name_parts.append(first_token.value)
        
        # Verificar se há mais partes (ex: .notas)
        while self._check(TokenType.DOT):
            self._advance()  # Consumir o ponto
            next_token = self._advance()
            if next_token.type != TokenType.IDENTIFIER:
                self._error("Esperado identificador após '.'")
            array_name_parts.append(next_token.value)
        
        # Construir o nome completo
        array_name = ".".join(array_name_parts)
        
        if not self._match(TokenType.LBRACKET):
            self._error("Esperado '[' para acesso ao array")
            
        index = self._parse_expression()
        
        if not self._match(TokenType.RBRACKET):
            self._error("Esperado ']' após índice")
            
        if not self._match(TokenType.ASSIGN):
            self._error("Esperado '=' para atribuição")
            
        value = self._parse_expression()
        return ArrayAssignmentNode(array_name, index, value)
    
    def _parse_array_field_assignment(self) -> ArrayFieldAssignmentNode:
        """Parse atribuição de campo de struct em array: array[index].campo = valor"""
        # Primeiro identificador (nome do array)
        array_token = self._advance()
        if array_token.type != TokenType.IDENTIFIER:
            self._error("Esperado identificador para nome do array")
        
        array_name = array_token.value
        
        if not self._match(TokenType.LBRACKET):
            self._error("Esperado '[' para acesso ao array")
            
        index = self._parse_expression()
        
        if not self._match(TokenType.RBRACKET):
            self._error("Esperado ']' após índice")
        
        if not self._match(TokenType.DOT):
            self._error("Esperado '.' após acesso ao array")
        
        # Coletar todos os campos do caminho
        field_path = []
        
        field_name = self._advance()
        if field_name.type != TokenType.IDENTIFIER:
            self._error("Esperado nome do campo após '.'")
        field_path.append(field_name.value)
        
        # Verificar se há mais níveis de acesso (ex: array[0].pessoa.nome)
        while self._match(TokenType.DOT):
            next_field = self._advance()
            if next_field.type != TokenType.IDENTIFIER:
                self._error("Esperado nome do campo após '.'")
            field_path.append(next_field.value)
        
        if not self._match(TokenType.ASSIGN):
            self._error("Esperado '=' após nome do campo")
        
        value = self._parse_expression()
        
        return ArrayFieldAssignmentNode(array_name, index, field_path, value)
    
    def _parse_print(self) -> PrintNode:
        if not self._match(TokenType.LPAREN):
            self._error("Esperado '(' após 'print'")
            
        expr = self._parse_expression()
        
        if not self._match(TokenType.RPAREN):
            self._error("Esperado ')' após expressão")
            
        return PrintNode(expr)
    
    def _parse_if(self) -> IfNode:
        condition = self._parse_expression()
        
        if not self._match(TokenType.THEN):
            self._error_at_current("Esperado 'then' após condição")
            
        then_branch = []
        while not self._check(TokenType.END) and not self._check(TokenType.ELSE) and not self._is_at_end():
            stmt = self._parse_statement()
            if stmt:
                then_branch.append(stmt)
        
        else_branch = None
        if self._match(TokenType.ELSE):
            else_branch = []
            while not self._check(TokenType.END) and not self._is_at_end():
                stmt = self._parse_statement()
                if stmt:
                    else_branch.append(stmt)
        
        if not self._match(TokenType.END):
            self._error_at_current("Esperado 'end' para fechar 'if'")
                
        return IfNode(condition, then_branch, else_branch)
    
    def _parse_while(self) -> WhileNode:
        condition = self._parse_expression()
        
        if not self._match(TokenType.DO):
            self._error_at_current("Esperado 'do' após condição")
            
        body = []
        while not self._match(TokenType.END) and not self._is_at_end():
            stmt = self._parse_statement()
            if stmt:
                body.append(stmt)
                
        return WhileNode(condition, body)
    
    def _parse_function(self) -> FunctionNode:
        name = self._advance()
        if name.type != TokenType.IDENTIFIER:
            self._error_at_current("Esperado nome da função")
            
        if not self._match(TokenType.LPAREN):
            self._error_at_current("Esperado '(' após nome da função")
            
        params = []
        while not self._check(TokenType.RPAREN) and not self._is_at_end():
            param_name = self._advance()
            if param_name.type != TokenType.IDENTIFIER:
                self._error_at_previous("Esperado nome do parâmetro")
            
            if not self._match(TokenType.COLON):
                self._error_at_current("Esperado ':' após nome do parâmetro")
            
            param_type = self._parse_type()
            params.append((param_name.value, param_type))
            
            if not self._check(TokenType.RPAREN):
                if not self._match(TokenType.COMMA):
                    self._error_at_current("Esperado ',' entre parâmetros")
        
        if not self._match(TokenType.RPAREN):
            self._error_at_current("Esperado ')' após parâmetros")
        
        # Tipo de retorno
        return_type = VoidType()
        if self._match(TokenType.ARROW):
            return_type = self._parse_type()
            
        # Entrando em escopo de função
        self.in_function_depth += 1
        body = []
        while not self._match(TokenType.END) and not self._is_at_end():
            stmt = self._parse_statement()
            if stmt:
                body.append(stmt)
        # Saindo do escopo de função
        self.in_function_depth -= 1
        
        # Registrar a função como definida
        self.defined_functions.add(name.value)
                
        return FunctionNode(name.value, params, return_type, body)
    
    def _parse_return(self) -> ReturnNode:
        value = None
        if not self._check(TokenType.SEMICOLON) and not self._check(TokenType.END):
            value = self._parse_expression()
        return ReturnNode(value)
    
    def _parse_break(self) -> BreakNode:
        """Parse a keyword break"""
        token = self.tokens[self.position - 1]  # Token 'break' já foi consumido
        node = BreakNode()
        return self._add_location_info(node, token)
    
    def _parse_use(self) -> UseNode:
        """Parse uma instrução use: use module [select symbol1, symbol2, ...] ou use module select *"""
        use_token = self.tokens[self.position - 1]  # Token 'use' já foi consumido
        
        # Parse o nome do módulo (suporta pacotes aninhados: utils.math)
        if not self._check(TokenType.IDENTIFIER):
            self._error("Nome do módulo esperado após 'use'")
        
        module_name = self._advance().value
        
        # Suporte para pacotes aninhados (utils.math.advanced)
        while self._match(TokenType.DOT):
            if not self._check(TokenType.IDENTIFIER):
                self._error("Nome do módulo esperado após '.'")
            module_name += "." + self._advance().value
        
        # Verifica se há uma cláusula 'select'
        if self._match(TokenType.SELECT):
            if self._match(TokenType.MULTIPLY):  # *
                # Import all: use module select *
                node = UseNode(module_name=module_name, import_all=True)
            else:
                # Import specific symbols: use module select symbol1, symbol2, ...
                selected_symbols = []
                
                # Primeiro símbolo
                if not self._check(TokenType.IDENTIFIER):
                    self._error("Nome do símbolo esperado após 'select'")
                selected_symbols.append(self._advance().value)
                
                # Símbolos adicionais separados por vírgula
                while self._match(TokenType.COMMA):
                    if not self._check(TokenType.IDENTIFIER):
                        self._error("Nome do símbolo esperado após ','")
                    selected_symbols.append(self._advance().value)
                
                node = UseNode(module_name=module_name, selected_symbols=selected_symbols)
        else:
            # Import whole module: use module
            node = UseNode(module_name=module_name)
        
        return self._add_location_info(node, use_token)
    
    def _parse_expression(self) -> ASTNode:
        return self._parse_or()
    
    def _parse_or(self) -> ASTNode:
        """Parse expressões OR: left || right"""
        left = self._parse_and()
        
        while self._match(TokenType.OR):
            operator = self.tokens[self.position - 1].type
            right = self._parse_and()
            left = BinaryOpNode(left, operator, right)
            
        return left
    
    def _parse_and(self) -> ASTNode:
        """Parse expressões AND: left && right"""
        left = self._parse_comparison()
        
        while self._match(TokenType.AND):
            operator = self.tokens[self.position - 1].type
            right = self._parse_comparison()
            left = BinaryOpNode(left, operator, right)
            
        return left
    
    def _parse_comparison(self) -> ASTNode:
        left = self._parse_term()
        
        while self._match(TokenType.GT, TokenType.LT, TokenType.GTE, TokenType.LTE, TokenType.EQ, TokenType.NEQ):
            operator = self.tokens[self.position - 1].type
            right = self._parse_term()
            left = BinaryOpNode(left, operator, right)
            
        return left
    
    def _parse_term(self) -> ASTNode:
        left = self._parse_factor()
        
        while self._match(TokenType.PLUS, TokenType.MINUS, TokenType.CONCAT):
            operator = self.tokens[self.position - 1].type
            right = self._parse_factor()
            if operator == TokenType.CONCAT:
                # Para concatenação de strings, usar o operador + que será tratado como concatenação
                left = BinaryOpNode(left, TokenType.PLUS, right)
            else:
                left = BinaryOpNode(left, operator, right)
            
        return left
    
    def _parse_factor(self) -> ASTNode:
        left = self._parse_unary()
        
        while self._match(TokenType.MULTIPLY, TokenType.DIVIDE, TokenType.MODULO):
            operator = self.tokens[self.position - 1].type
            right = self._parse_unary()
            left = BinaryOpNode(left, operator, right)
        
        return left
    
    def _parse_unary(self) -> ASTNode:
        if self._match(TokenType.MINUS):
            expr = self._parse_unary()
            return BinaryOpNode(NumberNode(0), TokenType.MINUS, expr)
        elif self._match(TokenType.NOT):
            expr = self._parse_unary()
            return UnaryOpNode(TokenType.NOT, expr)
        elif self._match(TokenType.REF):
            expr = self._parse_unary()
            return ReferenceNode(expr)
            
        return self._parse_postfix()
    
    def _parse_postfix(self) -> ASTNode:
        expr = self._parse_primary()
        
        while True:
            if self._match(TokenType.LBRACKET):
                index = self._parse_expression()
                if not self._match(TokenType.RBRACKET):
                    self._error_at_current("Esperado ']' após índice")
                
                # Verificar se é acesso a string literal
                if isinstance(expr, StringNode):
                    # Criar um nó especial para acesso a caractere de string
                    expr = StringCharAccessNode(expr.value, index)
                elif isinstance(expr, IdentifierNode):
                    expr = ArrayAccessNode(expr.name, index)
                elif isinstance(expr, StructAccessNode):
                    # Acesso a array de campo de struct: struct.campo[indice]
                    expr = ArrayAccessNode(f"{expr.struct_name}.{expr.field_name}", index)
                else:
                    self._error_at_current("Acesso de array inválido")
            elif self._match(TokenType.DOT):
                # Acesso a campo de struct ou chamada de função de módulo
                field_name = self._advance()
                if field_name.type != TokenType.IDENTIFIER:
                    self._error_at_previous("Esperado nome do campo após '.'")

                # Verificar se é uma chamada de função primeiro
                if self._check(TokenType.LPAREN):
                    # Construir nome da função baseado no tipo de expr
                    if isinstance(expr, IdentifierNode):
                        function_name = f"{expr.name}.{field_name.value}"
                    else:
                        function_name = field_name.value
                    
                    # É uma chamada de função: module.function(args) ou struct.method(args)
                    self._advance()  # Consumir LPAREN
                    args = []
                    if not self._check(TokenType.RPAREN):
                        args.append(self._parse_expression())
                        while self._match(TokenType.COMMA):
                            args.append(self._parse_expression())
                    
                    if not self._match(TokenType.RPAREN):
                        self._error_at_current("Esperado ')' após argumentos da função")
                    
                    # Criar CallNode com nome composto
                    call_node = CallNode(function_name, args)
                    call_node.line = field_name.line
                    call_node.column = field_name.column
                    expr = call_node
                
                # Se não é função, então é acesso a variável/propriedade
                elif isinstance(expr, IdentifierNode):
                    # Verificar se é acesso simples (struct) ou composto (module)
                    if '.' in expr.name:
                        # Acesso de módulo/variável composto: utils.math.pi
                        dotted_name = f"{expr.name}.{field_name.value}"
                        temp_identifier = IdentifierNode(dotted_name)
                        temp_identifier.line = field_name.line
                        temp_identifier.column = field_name.column
                        expr = temp_identifier
                    else:
                        # Verificar se é um namespace importado ou struct access
                        dotted_name = f"{expr.name}.{field_name.value}"
                        
                        # Para diferenciar entre namespace e struct, usar heurística:
                        # se nome do módulo é comum (utils, math, etc) ou já é composto, tratar como namespace
                        if (expr.name in ['utils', 'math', 'advanced', 'algorithms'] or 
                            '.' in expr.name):  # Já é um nome composto
                            # Provavelmente um namespace/módulo
                            temp_identifier = IdentifierNode(dotted_name)
                            temp_identifier.line = field_name.line
                            temp_identifier.column = field_name.column
                            expr = temp_identifier
                        else:
                            # Provavelmente um struct regular
                            struct_access = StructAccessNode(expr.name, field_name.value)
                            struct_access.line = field_name.line
                            struct_access.column = field_name.column
                            expr = struct_access
                elif isinstance(expr, StructAccessNode):
                    # Decidir se é namespace ou struct aninhado baseado no struct_name
                    if (expr.struct_name in ['utils', 'math', 'advanced', 'algorithms'] or
                        hasattr(expr, 'dotted_name')):
                        # É um namespace/módulo - criar IdentifierNode composto
                        if hasattr(expr, 'dotted_name'):
                            full_name = f"{expr.dotted_name}.{field_name.value}"
                        else:
                            full_name = f"{expr.struct_name}.{expr.field_name}.{field_name.value}"
                        
                        temp_identifier = IdentifierNode(full_name)
                        temp_identifier.line = field_name.line
                        temp_identifier.column = field_name.column
                        expr = temp_identifier
                    else:
                        # É um struct aninhado regular - manter como StructAccessNode
                        full_path = f"{expr.field_name}.{field_name.value}"
                        struct_access = StructAccessNode(expr.struct_name, full_path)
                        struct_access.line = field_name.line
                        struct_access.column = field_name.column
                        expr = struct_access
                elif isinstance(expr, ArrayAccessNode):
                    # Acesso: pessoas[i].campo (armazenar caminho como string)
                    expr = StructAccessFromArrayNode(expr, field_name.value)
                elif isinstance(expr, StructAccessFromArrayNode):
                    # Aninhar caminho: pessoas[i].endereco.rua
                    expr = StructAccessFromArrayNode(expr.base_access, f"{expr.field_path}.{field_name.value}")
                else:
                    self._error_at_current("Acesso a campo de struct inválido")
            elif self._match(TokenType.LPAREN):
                # Verificar se é cast, construtor de struct ou chamada de função
                if isinstance(expr, IdentifierNode):
                    # Verificar se é um tipo (para cast)
                    if expr.name in ['int', 'float', 'string', 'str', 'bool']:
                        # É um cast
                        cast_expr = self._parse_expression()
                        if not self._match(TokenType.RPAREN):
                            self._error_at_current("Esperado ')' após expressão de cast")
                        
                        target_type = None
                        if expr.name == 'int':
                            target_type = IntType()
                        elif expr.name == 'float':
                            target_type = FloatType()
                        elif expr.name in ['string', 'str']:
                            target_type = StringType()
                        elif expr.name == 'bool':
                            target_type = BoolType()
                        
                        expr = CastNode(cast_expr, target_type)
                    else:
                        # Verificar se é um construtor de struct
                        # Para isso, precisamos verificar se o nome existe como struct
                        # Por enquanto, vamos assumir que se não for uma função conhecida, é um construtor
                        args = []
                        while not self._check(TokenType.RPAREN) and not self._is_at_end():
                            args.append(self._parse_expression())
                            if not self._check(TokenType.RPAREN):
                                if not self._match(TokenType.COMMA):
                                    self._error_at_current("Esperado ',' entre argumentos")
                        
                        if not self._match(TokenType.RPAREN):
                            self._error_at_current("Esperado ')' após argumentos")
                        
                        # Verificar se é uma função conhecida ou definida
                        if expr.name in ['printf', 'malloc', 'free', 'strlen', 'strcpy', 'strcat', 'to_str', 'array_to_str', 'to_int', 'to_float', 'ord', 'length'] or expr.name in self.defined_functions:
                            call_node = CallNode(expr.name, args)
                            call_node.line = expr.line
                            call_node.column = expr.column
                            expr = call_node
                        elif expr.name in self.defined_structs:
                            # É um construtor de struct
                            expr = StructConstructorNode(expr.name, args)
                        else:
                            # Por padrão, assumir que é uma função (pode ser uma função não definida ainda)
                            call_node = CallNode(expr.name, args)
                            call_node.line = expr.line
                            call_node.column = expr.column
                            expr = call_node
                else:
                    self._error_at_current("Chamada de função inválida")
            else:
                break
                
        return expr
    
    def _parse_fstring(self) -> FStringNode:
        """Parse uma f-string e suas expressões embutidas"""
        token = self._current_token()
        parts_data = self._advance().value  # Lista de partes da f-string
        
        parsed_parts = []
        
        for part in parts_data:
            if isinstance(part, str):
                # Parte de string literal
                parsed_parts.append(part)
            elif isinstance(part, tuple) and part[0] == 'EXPR':
                # Parte de expressão - fazer parse da expressão
                expr_text = part[1]
                
                # Criar um lexer temporário para a expressão
                temp_lexer = Lexer(expr_text)
                try:
                    expr_tokens = temp_lexer.tokenize()
                    # Remover token EOF
                    expr_tokens = [t for t in expr_tokens if t.type != TokenType.EOF]
                    
                    if expr_tokens:
                        # Criar parser temporário para a expressão
                        temp_parser = Parser(expr_tokens + [Token(TokenType.EOF, "", 1, 1)], self.source_lines)
                        expr_node = temp_parser._parse_expression()
                        parsed_parts.append(expr_node)
                    else:
                        # Expressão vazia - erro
                        self._error_at_current("Expressão vazia em f-string")
                        
                except Exception as e:
                    self._error_at_current(f"Erro ao parsear expressão em f-string: {str(e)}")
            
            elif isinstance(part, tuple) and part[0] == 'EXPR_FORMAT':
                # Parte de expressão com formato - fazer parse da expressão
                expr_text = part[1]
                format_spec = part[2]
                
                # Criar um lexer temporário para a expressão
                temp_lexer = Lexer(expr_text)
                try:
                    expr_tokens = temp_lexer.tokenize()
                    # Remover token EOF
                    expr_tokens = [t for t in expr_tokens if t.type != TokenType.EOF]
                    
                    if expr_tokens:
                        # Criar parser temporário para a expressão
                        temp_parser = Parser(expr_tokens + [Token(TokenType.EOF, "", 1, 1)], self.source_lines)
                        expr_node = temp_parser._parse_expression()
                        # Adicionar informação de formato ao nó
                        expr_node.format_spec = format_spec
                        parsed_parts.append(expr_node)
                    else:
                        # Expressão vazia - erro
                        self._error_at_current("Expressão vazia em f-string")
                        
                except Exception as e:
                    self._error_at_current(f"Erro ao parsear expressão formatada em f-string: {str(e)}")
        
        node = FStringNode(parsed_parts)
        return self._add_location_info(node, token)
    
    def _parse_primary(self) -> ASTNode:
        if self._current_token().type == TokenType.NUMBER:
            return NumberNode(self._advance().value)
            
        if self._current_token().type == TokenType.FLOAT:
            return FloatNode(self._advance().value)
            
        if self._current_token().type == TokenType.STRING:
            return StringNode(self._advance().value)
            
        if self._current_token().type == TokenType.FSTRING:
            return self._parse_fstring()
            
        if self._current_token().type == TokenType.TRUE:
            self._advance()
            return BooleanNode(True)
            
        if self._current_token().type == TokenType.FALSE:
            self._advance()
            return BooleanNode(False)
            
        if self._current_token().type == TokenType.NULL:
            self._advance()
            return NullNode()
            
        if self._current_token().type == TokenType.BOOL:
            # Para casts como bool(1), retornar um IdentifierNode
            token = self._current_token()
            node = IdentifierNode(self._advance().value)
            return self._add_location_info(node, token)
            
        if self._current_token().type == TokenType.IDENTIFIER:
            token = self._current_token()
            return self._create_node_with_location(IdentifierNode, self._advance().value, token=token)
            
        if self._match(TokenType.LPAREN):
            expr = self._parse_expression()
            if not self._match(TokenType.RPAREN):
                self._error_at_current("Esperado ')' após expressão")
            return expr
            
        if self._match(TokenType.LBRACKET):
            # Array literal
            elements = []
            while not self._check(TokenType.RBRACKET) and not self._is_at_end():
                elements.append(self._parse_expression())
                if not self._check(TokenType.RBRACKET):
                    if not self._match(TokenType.COMMA):
                        self._error_at_current("Esperado ',' entre elementos do array")
            if not self._match(TokenType.RBRACKET):
                self._error_at_current("Esperado ']' para fechar array")
            # Inferir tipo do array baseado nos elementos
            if elements:
                if isinstance(elements[0], FloatNode):
                    return ArrayNode(elements, FloatType())
                elif isinstance(elements[0], StringNode):
                    return ArrayNode(elements, StringType())
                elif isinstance(elements[0], BooleanNode):
                    return ArrayNode(elements, BoolType())
                elif isinstance(elements[0], StructConstructorNode):
                    # Array de structs - usar o tipo do struct
                    struct_name = elements[0].struct_name
                    if struct_name in self.struct_types:
                        struct_type = self.struct_types[struct_name]
                        return ArrayNode(elements, struct_type)
                    else:
                        # Struct não definido ainda - criar tipo temporário
                        struct_type = StructType(struct_name, {})
                        return ArrayNode(elements, struct_type)
                else:
                    return ArrayNode(elements, IntType())
            else:
                return ArrayNode(elements, IntType())
        
        if self._match(TokenType.ZEROS):
            # Syntax sugar para arrays preenchidos com zeros
            if not self._match(TokenType.LPAREN):
                self._error_at_current("Esperado '(' após 'zeros'")
            
            if self._current_token().type != TokenType.NUMBER:
                self._error_at_current("Esperado tamanho do array")
            
            size = self._advance().value
            
            if not self._match(TokenType.RPAREN):
                self._error_at_current("Esperado ')' após tamanho")
            
            return ZerosNode(size, IntType())
            
        token = self._current_token()
        expected_tokens = ["número", "string", "identificador", "true", "false", "null", "(", "[", "zeros"]
        self._error(f"Token inesperado '{token.value}'. Esperado: {', '.join(expected_tokens)}")
    
    def _parse_struct_definition(self) -> StructDefinitionNode:
        """Parse definição de struct: struct Nome campo1: tipo1, campo2: tipo2, ... end"""
        if not self._check(TokenType.IDENTIFIER):
            self._error_at_current("Esperado nome do struct após 'struct'")
        
        name = self._advance()
        
        fields = []
        while not self._check(TokenType.END) and not self._is_at_end():
            if not self._check(TokenType.IDENTIFIER):
                self._error_at_current("Esperado nome do campo")
            
            field_name = self._advance()
            
            if not self._match(TokenType.COLON):
                self._error_at_current("Esperado ':' após nome do campo")
            
            field_type = self._parse_type()
            fields.append((field_name.value, field_type))
            
            if not self._check(TokenType.END):
                if not self._match(TokenType.COMMA):
                    self._error_at_current("Esperado ',' entre campos ou 'end' para fechar struct")
        
        if not self._match(TokenType.END):
            self._error_at_current("Esperado 'end' para fechar struct")
        
        # Criar o tipo de struct e armazená-lo
        struct_type = StructType(name.value, {field_name: field_type for field_name, field_type in fields})
        self.struct_types[name.value] = struct_type
        
        # Registrar o struct como definido
        self.defined_structs.add(name.value)
        
        return StructDefinitionNode(name.value, fields)
    
    def _parse_struct_assignment(self) -> Union[StructAssignmentNode, NestedStructAssignmentNode]:
        """Parse atribuição de campo de struct: struct.campo = valor ou struct.campo.subcampo = valor"""
        struct_name = self._advance()
        if struct_name.type != TokenType.IDENTIFIER:
            self._error_at_previous("Esperado nome do struct")
        
        if not self._match(TokenType.DOT):
            self._error_at_current("Esperado '.' após nome do struct")
        
        field_name = self._advance()
        if field_name.type != TokenType.IDENTIFIER:
            self._error_at_previous("Esperado nome do campo após '.'")
        
        # Coletar todos os campos do caminho
        field_path = [field_name.value]
        
        # Verificar se há mais níveis de acesso (ex: pessoa.endereco.rua)
        while self._match(TokenType.DOT):
            next_field = self._advance()
            if next_field.type != TokenType.IDENTIFIER:
                self._error_at_previous("Esperado nome do campo após '.'")
            field_path.append(next_field.value)
        
        if not self._match(TokenType.ASSIGN):
            self._error_at_current("Esperado '=' após nome do campo")
        
        value = self._parse_expression()
        
        # Se há apenas um campo, usar StructAssignmentNode (compatibilidade)
        if len(field_path) == 1:
            return StructAssignmentNode(struct_name.value, field_path[0], value)
        else:
            # Se há múltiplos campos, usar NestedStructAssignmentNode
            return NestedStructAssignmentNode(struct_name.value, field_path, value)

# Gerador de código LLVM
class CodeGenContext:
    """Context manager para geração de código com captura automática de erros"""
    def __init__(self, generator: 'LLVMCodeGenerator', node: ASTNode = None):
        self.generator = generator
        self.node = node
        
    def __enter__(self):
        self.generator._current_node = self.node
        return self
        
    def __exit__(self, exc_type, exc_val, exc_tb):
        if exc_type and not issubclass(exc_type, (NoxyError,)):
            # Capturar erros de geração de código e adicionar localização
            if self.node and self.node.has_location():
                source_line = self.node.source_line or (
                    self.generator.source_lines[self.node.line - 1] 
                    if self.node.line <= len(self.generator.source_lines)
                    else ""
                )
                raise NoxyCodeGenError(str(exc_val), self.node.line, self.node.column, source_line) from exc_val
            else:
                raise NoxyCodeGenError(str(exc_val)) from exc_val
        self.generator._current_node = None
        return False

class LLVMCodeGenerator:
    def __init__(self, source_lines: List[str] = None, imported_symbols: Dict[str, Any] = None):
        # Inicializar LLVM
        llvm.initialize()
        llvm.initialize_native_target()
        llvm.initialize_native_asmprinter()
        
        # Manter linhas de código fonte para contexto de erro
        self.source_lines = source_lines or []
        self._current_node = None  # Nó atual sendo processado
        self.imported_symbols = imported_symbols or {}  # Símbolos importados
        self.imported_functions_to_generate = set()  # Funções importadas que precisam ter corpo gerado
        
        # Obter o triple da plataforma atual; ajustar para MinGW/GCC quando necessário
        default_triple = llvm.get_default_triple()
        # Se for MSVC triple, ajuste para GNU (MinGW) para permitir link com gcc
        if default_triple.endswith("-pc-windows-msvc"):
            self.triple = default_triple.replace("-pc-windows-msvc", "-w64-windows-gnu")
        else:
            self.triple = default_triple
        
        # Criar módulo e builder
        self.module = ir.Module(name="noxy_module")
        self.module.triple = self.triple
        
        # Configurar data layout baseado na plataforma (preferir código estático para GCC/MinGW)
        target = llvm.Target.from_triple(self.triple)
        try:
            target_machine = target.create_target_machine(reloc='static', codemodel='large', opt=2)
        except TypeError:
            target_machine = target.create_target_machine(opt=2)
        self.target_data = target_machine.target_data
        self.module.data_layout = str(self.target_data)
        
        self.builder = None
        self.local_vars = {}
        self.global_vars = {}
        self.functions = {}
        self.current_function = None
        self.current_function_ast = None  # AST da função atual
        self.global_ast = None  # AST global do programa
        self.type_map = {}
        self.struct_types = {}  # Armazenar tipos de struct LLVM
        
        # Sistema de gestão de memória
        self.allocated_ptrs = []  # Lista de ponteiros alocados para liberação
        self.memory_tracking = True  # Habilitar rastreamento de memória
        self.allocation_array = None  # Array para armazenar ponteiros alocados
        self.allocation_count = None  # Contador de alocações
        
        # Tipos básicos LLVM
        self.int_type = ir.IntType(64)
        self.float_type = ir.DoubleType()
        self.char_type = ir.IntType(8)
        self.string_type = ir.IntType(8).as_pointer()  # Garante i8*
        self.void_type = ir.VoidType()
        self.bool_type = ir.IntType(1)
        # Indicador de geração no nível top (código do arquivo dentro de main)
        self.in_top_level = False
        
        # Declarar funções externas
        self._declare_external_functions()
    
    def _semantic_error(self, message: str, node: ASTNode = None) -> None:
        """Lança um erro semântico com informações de linha e coluna"""
        if node and node.line is not None and node.column is not None:
            if node.line <= len(self.source_lines):
                source_line = self.source_lines[node.line - 1]
                raise NoxySemanticError(message, node.line, node.column, source_line)
        raise NoxySemanticError(message)
    
    def _with_context(self, node: ASTNode = None):
        """Retorna context manager para geração de código com localização"""
        return CodeGenContext(self, node)
        
    def _declare_external_functions(self):
        # printf
        voidptr_ty = ir.IntType(8).as_pointer()
        printf_ty = ir.FunctionType(ir.IntType(32), [voidptr_ty], var_arg=True)
        self.printf = ir.Function(self.module, printf_ty, name="printf")
        
        # Para melhor suporte a Unicode no Windows, também declarar wprintf
        if sys.platform == "win32":
            # wprintf para wide chars
            wchar_ptr_ty = ir.IntType(16).as_pointer()
            wprintf_ty = ir.FunctionType(ir.IntType(32), [wchar_ptr_ty], var_arg=True)
            self.wprintf = ir.Function(self.module, wprintf_ty, name="wprintf")
            
            # _setmode para configurar o modo do console
            setmode_ty = ir.FunctionType(ir.IntType(32), [ir.IntType(32), ir.IntType(32)])
            self.setmode = ir.Function(self.module, setmode_ty, name="_setmode")
            
            # SetConsoleOutputCP
            setcp_ty = ir.FunctionType(ir.IntType(32), [ir.IntType(32)])
            self.setconsolecp = ir.Function(self.module, setcp_ty, name="SetConsoleOutputCP")
        
        # malloc
        malloc_ty = ir.FunctionType(voidptr_ty, [ir.IntType(64)])
        self.malloc = ir.Function(self.module, malloc_ty, name="malloc")
        
        # free
        free_ty = ir.FunctionType(self.void_type, [voidptr_ty])
        self.free = ir.Function(self.module, free_ty, name="free")
        
        # strlen
        strlen_ty = ir.FunctionType(ir.IntType(64), [voidptr_ty])
        self.strlen = ir.Function(self.module, strlen_ty, name="strlen")
        
        # strcpy
        strcpy_ty = ir.FunctionType(voidptr_ty, [voidptr_ty, voidptr_ty])
        self.strcpy = ir.Function(self.module, strcpy_ty, name="strcpy")
        
        # strcat
        strcat_ty = ir.FunctionType(voidptr_ty, [voidptr_ty, voidptr_ty])
        self.strcat = ir.Function(self.module, strcat_ty, name="strcat")
        
        # strcmp
        strcmp_ty = ir.FunctionType(ir.IntType(32), [voidptr_ty, voidptr_ty])
        self.strcmp = ir.Function(self.module, strcmp_ty, name="strcmp")
        
        # sprintf para conversões
        sprintf_ty = ir.FunctionType(ir.IntType(32), [voidptr_ty, voidptr_ty], var_arg=True)
        self.sprintf = ir.Function(self.module, sprintf_ty, name="sprintf")
        
        # Funções de casting
        # to_str: converte int/float para string (sobrecarregada)
        to_str_int_ty = ir.FunctionType(self.string_type, [self.int_type])
        self.to_str_int = ir.Function(self.module, to_str_int_ty, name="to_str_int")
        to_str_float_ty = ir.FunctionType(self.string_type, [self.float_type])
        self.to_str_float = ir.Function(self.module, to_str_float_ty, name="to_str_float")
        
        # array_to_str: converte arrays para string
        array_to_str_int_ty = ir.FunctionType(self.string_type, [self.int_type.as_pointer(), self.int_type])
        self.array_to_str_int = ir.Function(self.module, array_to_str_int_ty, name="array_to_str_int")
        array_to_str_float_ty = ir.FunctionType(self.string_type, [self.float_type.as_pointer(), self.int_type])
        self.array_to_str_float = ir.Function(self.module, array_to_str_float_ty, name="array_to_str_float")
        
        # to_int: converte float para int
        to_int_ty = ir.FunctionType(self.int_type, [self.float_type])
        self.to_int = ir.Function(self.module, to_int_ty, name="to_int")
        
        # to_float: converte int para float
        to_float_ty = ir.FunctionType(self.float_type, [self.int_type])
        self.to_float = ir.Function(self.module, to_float_ty, name="to_float")
        
        # char_to_str: converte caractere para string
        char_to_str_ty = ir.FunctionType(self.string_type, [self.char_type])
        self.char_to_str = ir.Function(self.module, char_to_str_ty, name="char_to_str")
        
        if sys.platform == "win32":
            # Adicionar atributos para linking correto no Windows
            for func in [self.printf, self.malloc, self.free, self.strlen, self.strcpy, self.strcat, self.strcmp, self.sprintf, self.to_str_int, self.to_str_float, self.array_to_str_int, self.array_to_str_float, self.to_int, self.to_float, self.char_to_str]:
                if func:
                    func.calling_convention = 'ccc'
                    func.linkage = 'external'
            
            if hasattr(self, 'wprintf'):
                self.wprintf.calling_convention = 'ccc'
                self.wprintf.linkage = 'external'
            if hasattr(self, 'setmode'):
                self.setmode.calling_convention = 'ccc'
                self.setmode.linkage = 'external'
            if hasattr(self, 'setconsolecp'):
                self.setconsolecp.calling_convention = 'ccc'
                self.setconsolecp.linkage = 'external'
        else:
            # Adicionar atributos para outras plataformas
            for func in [self.printf, self.malloc, self.free, self.strlen, self.strcpy, self.strcat, self.strcmp, self.sprintf, self.to_str_int, self.to_str_float, self.array_to_str_int, self.array_to_str_float, self.to_int, self.to_float, self.char_to_str]:
                func.calling_convention = 'ccc'
                func.linkage = 'external'
        
        if sys.platform == "win32":
            # Adicionar atributos para linking correto no Windows
            for func in [self.printf, self.malloc, self.free, self.strlen, self.strcpy, self.to_str_int, self.to_str_float, self.array_to_str_int, self.array_to_str_float, self.to_int, self.to_float, self.char_to_str]:
                if func:
                    func.calling_convention = 'ccc'
                    func.linkage = 'external'
            
            if hasattr(self, 'wprintf'):
                self.wprintf.calling_convention = 'ccc'
                self.wprintf.linkage = 'external'
            if hasattr(self, 'setmode'):
                self.setmode.calling_convention = 'ccc'
                self.setmode.linkage = 'external'
            if hasattr(self, 'setconsolecp'):
                self.setconsolecp.calling_convention = 'ccc'
                self.setconsolecp.linkage = 'external'
    
        # fmod para float módulo
        fmod_ty = ir.FunctionType(self.float_type, [self.float_type, self.float_type])
        self.fmod = ir.Function(self.module, fmod_ty, name="fmod")
        
        # Adicionar funções built-in ao dicionário de funções para que sejam reconhecidas
        # Funções de string
        self.functions['strlen'] = self.strlen
        self.functions['strcpy'] = self.strcpy
        self.functions['strcat'] = self.strcat
        self.functions['strcmp'] = self.strcmp
        
        # Funções de memória
        self.functions['malloc'] = self.malloc
        self.functions['free'] = self.free
        
        # Funções de casting
        self.functions['to_str'] = self.to_str_int  # Usar versão int como padrão
        self.functions['array_to_str'] = self.array_to_str_int  # Usar versão int como padrão
        self.functions['to_int'] = self.to_int
        self.functions['to_float'] = self.to_float
        self.functions['char_to_str'] = self.char_to_str
        
        # Função de impressão
        self.functions['printf'] = self.printf
    
    def _convert_type(self, ml_type: Type) -> ir.Type:
        if isinstance(ml_type, IntType):
            return self.int_type
        elif isinstance(ml_type, FloatType):
            return self.float_type
        elif isinstance(ml_type, StringType) or isinstance(ml_type, StrType):
            return self.string_type
        elif isinstance(ml_type, BoolType):
            return self.bool_type
        elif isinstance(ml_type, ArrayType):
            if ml_type.size is not None:
                # Para arrays com tamanho definido, armazenar structs por valor
                if isinstance(ml_type.element_type, StructType):
                    struct_name = ml_type.element_type.name
                    if struct_name in self.struct_types:
                        struct_type = self.struct_types[struct_name]
                        return ir.ArrayType(struct_type, ml_type.size)
                    else:
                        # Struct não definido ainda, criar tipo opaco
                        opaque_type = ir.LiteralStructType([], packed=False)
                        self.struct_types[struct_name] = opaque_type
                        return ir.ArrayType(opaque_type, ml_type.size)
                else:
                    element_type = self._convert_type(ml_type.element_type)
                    return ir.ArrayType(element_type, ml_type.size)
            else:
                element_type = self._convert_type(ml_type.element_type)
                return element_type.as_pointer()
        elif isinstance(ml_type, VoidType):
            return self.void_type
        elif isinstance(ml_type, ReferenceType):
            # Para referências, converter o tipo alvo e retornar ponteiro
            if isinstance(ml_type.target_type, StructType):
                # Para referências a structs, verificar se o struct já foi definido
                if ml_type.target_type.name in self.struct_types:
                    return self.struct_types[ml_type.target_type.name].as_pointer()
                else:
                    # Struct não definido ainda, criar um tipo opaco
                    opaque_type = ir.LiteralStructType([], packed=False)
                    self.struct_types[ml_type.target_type.name] = opaque_type
                    return opaque_type.as_pointer()
            else:
                # Para outros tipos, converter normalmente
                target_type = self._convert_type(ml_type.target_type)
                return target_type.as_pointer()
        elif isinstance(ml_type, StructType):
            if ml_type.name in self.struct_types:
                return self.struct_types[ml_type.name].as_pointer()
            else:
                # Isso não deveria acontecer se a primeira passada foi executada corretamente
                raise TypeError(f"Struct '{ml_type.name}' não foi registrado")
        else:
            raise TypeError(f"Tipo não suportado: {ml_type}")
    
    def generate(self, ast: ProgramNode):
        # Armazenar AST global
        self.global_ast = ast
        # Geração preservará a ordem textual dos statements do topo
        
        # Coletar todas as definições de struct
        struct_definitions = []
        for stmt in ast.statements:
            if isinstance(stmt, StructDefinitionNode):
                struct_definitions.append(stmt)
        
        # Adicionar structs importados
        for symbol_name, symbol_info in self.imported_symbols.items():
            if symbol_info['type'] == 'struct':
                struct_definitions.append(symbol_info['node'])
        
        # Processar structs em ordem de dependência
        self._process_structs_in_dependency_order(struct_definitions)
        
        # Depois, declarar todas as variáveis globais e funções (deduplicado por identificador)
        declared_globals: set[str] = set()
        for stmt in ast.statements:
            if isinstance(stmt, AssignmentNode) and stmt.is_global:
                if stmt.identifier in declared_globals:
                    continue
                declared_globals.add(stmt.identifier)
                self._declare_global_variable(stmt)
            elif isinstance(stmt, FunctionNode):
                self._declare_function(stmt)
        
        # Declarar variáveis globais e funções importadas
        for symbol_name, symbol_info in self.imported_symbols.items():
            if symbol_info['type'] == 'variable':
                # Para variáveis, usar o nome simples
                simple_name = symbol_name.split('.')[-1] if '.' in symbol_name else symbol_name
                if simple_name not in self.global_vars:
                    var_node = symbol_info['node']
                    self._declare_global_variable(var_node)
            elif symbol_info['type'] == 'function':
                # Para funções, usar o nome simples para declaração
                simple_name = symbol_name.split('.')[-1] if '.' in symbol_name else symbol_name
                if simple_name not in self.functions:
                    func_node = symbol_info['node']
                    self._declare_function(func_node)
        
        # Criar função main
        main_ty = ir.FunctionType(ir.IntType(32), [])
        main_func = ir.Function(self.module, main_ty, name="main")
        
        # Criar bloco básico
        entry_block = main_func.append_basic_block(name="entry")
        self.builder = ir.IRBuilder(entry_block)
        self.current_function = main_func
        self.local_vars = {}
        self.in_top_level = True
        
        # Criar variáveis globais para rastreamento de memória se necessário
        if self.memory_tracking:
            # Verificar se já existem as variáveis globais
            if "allocation_array" not in self.module.globals:
                self.allocation_array = ir.GlobalVariable(
                    self.module,
                ir.ArrayType(self.char_type.as_pointer(), 100), 
                name="allocation_array"
            )
                self.allocation_array.linkage = 'internal'
                self.allocation_array.initializer = ir.Constant(
                    ir.ArrayType(self.char_type.as_pointer(), 100),
                    None
                )
            else:
                self.allocation_array = self.module.globals["allocation_array"]
                
            if "allocation_count" not in self.module.globals:
                self.allocation_count = ir.GlobalVariable(
                    self.module,
                self.int_type, 
                name="allocation_count"
            )
                self.allocation_count.linkage = 'internal'
                self.allocation_count.initializer = ir.Constant(self.int_type, 0)
            else:
                self.allocation_count = self.module.globals["allocation_count"]
            # Inicializar contador
            self.builder.store(ir.Constant(self.int_type, 0), self.allocation_count)
        
        # No Windows, configurar UTF-8 no início do programa
        if sys.platform == "win32":
            self._setup_windows_utf8()
        
        # Primeiro, gerar as variáveis globais importadas
        for symbol_name, symbol_info in self.imported_symbols.items():
            if symbol_info['type'] == 'variable':
                # Para variáveis, usar o nome simples para verificar
                simple_name = symbol_name.split('.')[-1] if '.' in symbol_name else symbol_name
                if simple_name in self.global_vars:
                    # Gerar a inicialização da variável global importada
                    var_node = symbol_info['node']
                    if var_node.value is not None:
                        self._generate_statement(var_node)
        
        # Em seguida, gerar as funções importadas
        for symbol_name, symbol_info in self.imported_symbols.items():
            if symbol_info['type'] == 'function':
                # Para funções, usar o nome simples para verificar se foi declarada
                simple_name = symbol_name.split('.')[-1] if '.' in symbol_name else symbol_name
                if simple_name in self.functions:
                    func_node = symbol_info['node']
                    self._generate_function(func_node)
        
        # Processar todos os statements (exceto definições de função) na ordem original
        for stmt in ast.statements:
            if not isinstance(stmt, FunctionNode):
                self._generate_statement(stmt)
        
        # Adicionar limpeza de memória antes do return
        if self.memory_tracking:
            self._add_memory_cleanup()
        
        # Retornar 0 se não houver return explícito
        if not self.builder.block.is_terminated:
            self.builder.ret(ir.Constant(ir.IntType(32), 0))
        
        # Sair do nível top-level
        self.in_top_level = False
        # Gerar código para as funções
        for stmt in ast.statements:
            if isinstance(stmt, FunctionNode):
                self._generate_function(stmt)
        
        return self.module
    
    def _track_allocation(self, ptr_value: ir.Value):
        """Rastreia uma alocação de memória para liberação posterior"""
        if self.memory_tracking and ptr_value:
            # Armazenar o ponteiro em uma variável local para garantir dominância
            if hasattr(self, 'current_function') and self.current_function:
                # Verificar se o array de alocações já foi criado
                if hasattr(self, 'allocation_array') and self.allocation_array is not None:
                    # Armazenar o ponteiro no array
                    count_ptr = self.builder.load(self.allocation_count)
                    array_elem_ptr = self.builder.gep(
                        self.allocation_array, 
                        [ir.Constant(self.int_type, 0), count_ptr], 
                        inbounds=True
                    )
                    self.builder.store(ptr_value, array_elem_ptr)
                    
                    # Incrementar contador
                    new_count = self.builder.add(count_ptr, ir.Constant(self.int_type, 1))
                    self.builder.store(new_count, self.allocation_count)
                else:
                    # Fallback para a lista antiga se o array não foi criado
                    self.allocated_ptrs.append(ptr_value)
            else:
                self.allocated_ptrs.append(ptr_value)
    
    def _add_memory_cleanup(self):
        """Adiciona código para liberar toda a memória alocada"""
        if not self.memory_tracking:
            return
        
        # Se temos um array de alocações, liberar todos os ponteiros nele
        if hasattr(self, 'allocation_array') and self.allocation_array is not None:
            # Carregar contador
            count_ptr = self.builder.load(self.allocation_count)
            
            # Criar loop para liberar todos os ponteiros
            i_ptr = self.builder.alloca(self.int_type, name="cleanup_i")
            self.builder.store(ir.Constant(self.int_type, 0), i_ptr)
            
            # Bloco de condição do loop
            cond_block = self.current_function.append_basic_block(name="cleanup_cond")
            body_block = self.current_function.append_basic_block(name="cleanup_body")
            end_block = self.current_function.append_basic_block(name="cleanup_end")
            
            self.builder.branch(cond_block)
            
            # Bloco de condição
            self.builder.position_at_end(cond_block)
            i_val = self.builder.load(i_ptr)
            condition = self.builder.icmp_signed('<', i_val, count_ptr)
            self.builder.cbranch(condition, body_block, end_block)
            
            # Bloco do corpo do loop
            self.builder.position_at_end(body_block)
            
            # Carregar ponteiro do array
            array_elem_ptr = self.builder.gep(
                self.allocation_array, 
                [ir.Constant(self.int_type, 0), i_val], 
                inbounds=True
            )
            ptr_to_free = self.builder.load(array_elem_ptr)
            
            # Liberar o ponteiro
            self.builder.call(self.free, [ptr_to_free])
            
            # Incrementar i
            new_i = self.builder.add(i_val, ir.Constant(self.int_type, 1))
            self.builder.store(new_i, i_ptr)
            
            # Voltar para condição
            self.builder.branch(cond_block)
            
            # Continuar após o loop
            self.builder.position_at_end(end_block)
            
            # Limpar referências
            self.allocation_array = None
            self.allocation_count = None
        else:
            # Fallback para o método antigo
            if not self.allocated_ptrs:
                return
            
            # Liberar cada ponteiro alocado
            for ptr in self.allocated_ptrs:
                if isinstance(ptr.type, ir.PointerType):
                    # Se é uma variável local (alloca), carregar o valor
                    if isinstance(ptr, ir.AllocaInstr):
                        loaded_ptr = self.builder.load(ptr)
                        self.builder.call(self.free, [loaded_ptr])
                    else:
                        self.builder.call(self.free, [ptr])
            
            # Limpar a lista após liberação
            self.allocated_ptrs.clear()
    
    def _cleanup_function_memory(self):
        """Libera memória alocada dentro de uma função"""
        if not self.memory_tracking:
            return
        
        # Liberar variáveis locais que são ponteiros
        for var_name, var_ptr in self.local_vars.items():
            if isinstance(var_ptr, ir.AllocaInstr) and isinstance(var_ptr.type.pointee, ir.PointerType):
                # Carregar o valor do ponteiro
                loaded_ptr = self.builder.load(var_ptr)
                # Verificar se não é null antes de liberar
                null_ptr = ir.Constant(var_ptr.type.pointee, None)
                is_not_null = self.builder.icmp_ne(loaded_ptr, null_ptr)
                
                # Criar blocos para condição
                cleanup_block = self.current_function.append_basic_block(name=f"cleanup_{var_name}")
                continue_block = self.current_function.append_basic_block(name=f"continue_{var_name}")
                
                self.builder.cbranch(is_not_null, cleanup_block, continue_block)
                
                # Bloco de limpeza
                self.builder.position_at_end(cleanup_block)
                self.builder.call(self.free, [loaded_ptr])
                self.builder.branch(continue_block)
                
                # Continuar
                self.builder.position_at_end(continue_block)
    
    def _setup_windows_utf8(self):
        """Configura o console do Windows para UTF-8"""
        # Apenas chamar SetConsoleOutputCP(65001) para UTF-8
        if hasattr(self, 'setconsolecp'):
            utf8_codepage = ir.Constant(ir.IntType(32), 65001)
            self.builder.call(self.setconsolecp, [utf8_codepage])
    
    def _eval_constant(self, node):
        """Avalia um nó de valor constante para inicialização de globais."""
        if isinstance(node, NumberNode):
            return node.value
        elif isinstance(node, FloatNode):
            return node.value
        elif isinstance(node, StringNode):
            return node.value
        elif isinstance(node, BooleanNode):
            return node.value
        elif isinstance(node, NullNode):
            return 0  # null é representado como 0
        elif isinstance(node, ArrayNode):
            return [self._eval_constant(e) for e in node.elements]
        elif isinstance(node, ZerosNode):
            if isinstance(node.element_type, IntType):
                return [0] * node.size
            elif isinstance(node.element_type, FloatType):
                return [0.0] * node.size
            elif isinstance(node.element_type, StringType) or isinstance(node.element_type, StrType):
                return [None] * node.size
            elif isinstance(node.element_type, BoolType):
                return [False] * node.size
            else:
                return [None] * node.size
        elif isinstance(node, BinaryOpNode):
            # Suporte para expressões binárias simples em constantes
            left = self._eval_constant(node.left)
            right = self._eval_constant(node.right)
            
            if node.operator == TokenType.PLUS:
                return left + right
            elif node.operator == TokenType.MINUS:
                return left - right
            elif node.operator == TokenType.MULTIPLY:
                return left * right
            elif node.operator == TokenType.DIVIDE:
                return left / right
            elif node.operator == TokenType.AND:
                return left and right
            elif node.operator == TokenType.OR:
                return left or right
            else:
                raise Exception(f"Operador não suportado em constante global: {node.operator}")
        elif isinstance(node, CallNode):
            # Para chamadas de função, retornar None para indicar que precisa ser avaliada em runtime
            return None
        else:
            raise Exception(f"Valor inicial de global não suportado: {node}")

    def _declare_global_variable(self, node: AssignmentNode):
        """Declara uma variável global com valor neutro.
        A inicialização em tempo de execução ocorrerá em ordem textual via geração normal de statements."""
        # Evitar redefinição de globais com o mesmo nome
        if node.identifier in self.global_vars:
            # Se já existe, validar tipo (quando disponível) e ignorar nova declaração
            existing_type = self.type_map.get(node.identifier)
            if node.var_type is not None and existing_type is not None and type(existing_type) != type(node.var_type):
                raise TypeError(f"Redeclaração de global '{node.identifier}' com tipo diferente")
            # Mapear tipo se ainda não mapeado
            if existing_type is None and node.var_type is not None:
                self.type_map[node.identifier] = node.var_type
            return
        var_type = self._convert_type(node.var_type)
        
        # Não agendar chamadas para execução antecipada
        
        # Criar variável global com valor neutro (zero/null)
        if isinstance(node.var_type, ArrayType):
            # Arrays estáticos: inicializar zerados
            element_lltype = self._convert_type(node.var_type.element_type)
            array_type = ir.ArrayType(element_lltype, node.var_type.size or 0)
            gv = ir.GlobalVariable(self.module, array_type, name=node.identifier)
            gv.initializer = ir.Constant(array_type, None)
        else:
            gv = ir.GlobalVariable(self.module, var_type, name=node.identifier)
            if isinstance(node.var_type, IntType):
                gv.initializer = ir.Constant(var_type, 0)
            elif isinstance(node.var_type, FloatType):
                gv.initializer = ir.Constant(var_type, 0.0)
            elif isinstance(node.var_type, BoolType):
                gv.initializer = ir.Constant(var_type, 0)
            elif isinstance(node.var_type, (StringType, StrType)):
                gv.initializer = ir.Constant(var_type, None)
            else:
                gv.initializer = ir.Constant(var_type, None)
        
        gv.linkage = 'internal'
        self.global_vars[node.identifier] = gv
        self.type_map[node.identifier] = node.var_type
        
        # Não usar global_runtime_inits; inicialização acontecerá no fluxo normal
    
    def _declare_function(self, node: FunctionNode):
        # Converter tipos dos parâmetros com tratamento especial para referências
        param_types = []
        for param_name, param_type in node.params:
            if isinstance(param_type, ReferenceType) and isinstance(param_type.target_type, StructType):
                # Para referências a structs, usar ponteiro para o tipo correto
                if param_type.target_type.name in self.struct_types:
                    param_types.append(self.struct_types[param_type.target_type.name].as_pointer())
                else:
                    # Struct não definido, usar ponteiro para void temporariamente
                    param_types.append(ir.IntType(8).as_pointer())
            else:
                param_types.append(self._convert_type(param_type))
        
        # Converter tipo de retorno
        if isinstance(node.return_type, ReferenceType) and isinstance(node.return_type.target_type, StructType):
            if node.return_type.target_type.name in self.struct_types:
                return_type = self.struct_types[node.return_type.target_type.name].as_pointer()
            else:
                return_type = ir.IntType(8).as_pointer()
        else:
            return_type = self._convert_type(node.return_type)
        
        func_ty = ir.FunctionType(return_type, param_types)
        
        # Criar função
        func = ir.Function(self.module, func_ty, name=node.name)
        self.functions[node.name] = func
        
        # Armazenar informação sobre tipos
        self.type_map[node.name] = FunctionType([p[1] for p in node.params], node.return_type)
    
    def _validate_circular_references(self, struct_name: str, visited: set = None) -> bool:
        """
        Valida se um struct tem referências circulares válidas.
        Retorna True se as referências são válidas, False se há recursão infinita.
        """
        if visited is None:
            visited = set()
        
        # Evitar recursão infinita na validação
        if struct_name in visited:
            return True  # Referência circular válida (auto-referência)
        
        visited.add(struct_name)
        
        # Verificar se o struct existe
        if struct_name not in self.struct_field_types:
            return True  # Struct não definido ainda, assumir válido
        
        # Verificar cada campo do struct
        for field_name, field_type in self.struct_field_types[struct_name].items():
            if isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                target_name = field_type.target_type.name
                
                # Se é auto-referência, é válida
                if target_name == struct_name:
                    continue
                
                # Verificar se a referência é para um struct válido
                if target_name not in self.struct_field_types:
                    # Struct não definido, pode causar problemas
                    return False
                
                # Verificar recursivamente o struct alvo
                if not self._validate_circular_references(target_name, visited.copy()):
                    return False
        
        return True

    def _process_struct_definition(self, node: StructDefinitionNode):
        # Abordagem conservadora para evitar recursão infinita
        # Primeira passada: criar struct com placeholders para auto-referências
        field_types = []
        auto_references = []  # Lista para rastrear campos que são auto-referências
        
        for i, (field_name, field_type) in enumerate(node.fields):
            if isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                # Verificar se é auto-referência (mesmo nome do struct)
                if field_type.target_type.name == node.name:
                    # Auto-referência: usar ponteiro para void como placeholder
                    llvm_type = ir.IntType(8).as_pointer()  # void* equivalente
                    auto_references.append((i, field_name))
                else:
                    # Referência para outro struct: usar ponteiro para void também
                    # para evitar dependências circulares complexas
                    llvm_type = ir.IntType(8).as_pointer()  # void* equivalente
            elif isinstance(field_type, StructType):
                # Para outros structs, verifica se já existe
                if field_type.name not in self.struct_types:
                    # Cria um tipo opaco para forward declaration
                    other_opaque = ir.LiteralStructType([], packed=False)
                    self.struct_types[field_type.name] = other_opaque
                llvm_type = self.struct_types[field_type.name]
            else:
                llvm_type = self._convert_type(field_type)
            field_types.append(llvm_type)
        
        # Criar o struct com os tipos corretos
        actual_struct_type = ir.LiteralStructType(field_types, packed=False)
        self.struct_types[node.name] = actual_struct_type
        
        # Armazenar informações dos campos para acesso posterior
        if not hasattr(self, 'struct_fields'):
            self.struct_fields = {}
        self.struct_fields[node.name] = {field_name: i for i, (field_name, _) in enumerate(node.fields)}
        
        # Armazenar tipos dos campos para navegação aninhada
        if not hasattr(self, 'struct_field_types'):
            self.struct_field_types = {}
        self.struct_field_types[node.name] = {field_name: field_type for field_name, field_type in node.fields}
        
        # Armazenar informações sobre auto-referências para uso posterior
        if not hasattr(self, 'struct_auto_references'):
            self.struct_auto_references = {}
        if auto_references:
            self.struct_auto_references[node.name] = auto_references
    
    def _process_structs_in_dependency_order(self, struct_definitions):
        """Processa structs na ordem correta de dependências"""
        processed = set()
        processing = set()
        
        def process_struct(struct_def):
            if struct_def.name in processed:
                return
            if struct_def.name in processing:
                # Dependência circular detectada - processar com placeholders
                self._process_struct_with_placeholders(struct_def)
                processed.add(struct_def.name)
                processing.discard(struct_def.name)
                return
            
            processing.add(struct_def.name)
            
            # Encontrar dependências deste struct
            dependencies = self._get_struct_dependencies(struct_def)
            
            # Processar dependências primeiro
            for dep_name in dependencies:
                dep_struct = next((s for s in struct_definitions if s.name == dep_name), None)
                if dep_struct:
                    process_struct(dep_struct)
            
            # Processar este struct
            self._process_struct_definition(struct_def)
            processed.add(struct_def.name)
            processing.discard(struct_def.name)
        
        # Processar todos os structs
        for struct_def in struct_definitions:
            process_struct(struct_def)
    
    def _get_struct_dependencies(self, struct_def):
        """Obtém lista de structs dos quais este struct depende"""
        dependencies = set()
        for field_name, field_type in struct_def.fields:
            if isinstance(field_type, StructType):
                dependencies.add(field_type.name)
            elif isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                # Referências não são dependências diretas para ordem de processamento
                pass
        return dependencies
    
    def _process_struct_with_placeholders(self, struct_def):
        """Processa struct com placeholders para dependências circulares"""
        # Para dependências circulares, usar void* para campos problemáticos
        field_types = []
        for field_name, field_type in struct_def.fields:
            if isinstance(field_type, StructType) and field_type.name not in self.struct_types:
                # Usar void* como placeholder
                llvm_type = ir.IntType(8).as_pointer()
            else:
                llvm_type = self._convert_type(field_type)
            field_types.append(llvm_type)
        
        # Criar struct
        actual_struct_type = ir.LiteralStructType(field_types, packed=False)
        self.struct_types[struct_def.name] = actual_struct_type
        
        # Armazenar metadados
        if not hasattr(self, 'struct_fields'):
            self.struct_fields = {}
        self.struct_fields[struct_def.name] = {field_name: i for i, (field_name, _) in enumerate(struct_def.fields)}
        
        if not hasattr(self, 'struct_field_types'):
            self.struct_field_types = {}
        self.struct_field_types[struct_def.name] = {field_name: field_type for field_name, field_type in struct_def.fields}
    
    def _generate_function(self, node: FunctionNode):
        func = self.functions[node.name]
        
        # Criar bloco de entrada
        entry_block = func.append_basic_block(name="entry")
        
        # Salvar estado atual
        old_builder = self.builder
        old_vars = self.local_vars
        old_func = self.current_function
        old_func_ast = self.current_function_ast
        
        # Configurar novo contexto
        self.builder = ir.IRBuilder(entry_block)
        self.local_vars = {}
        self.current_function = func
        self.current_function_ast = node
        
        # Mapear parâmetros para variáveis locais
        for i, ((param_name, param_type), param) in enumerate(zip(node.params, func.args)):
            param.name = param_name
            # Se o parâmetro é um array (ponteiro), não criar alloca adicional
            if isinstance(param_type, ArrayType):
                self.local_vars[param_name] = param
                self.type_map[param_name] = param_type
            else:
                # Para tipos escalares, criar alloca e armazenar
                alloca = self.builder.alloca(param.type, name=param_name)
                self.builder.store(param, alloca)
                self.local_vars[param_name] = alloca
                self.type_map[param_name] = param_type
        
        # Gerar corpo da função
        for stmt in node.body:
            self._generate_statement(stmt)
        
        # Adicionar return padrão se necessário
        if not self.builder.block.is_terminated:
            if isinstance(node.return_type, VoidType):
                self.builder.ret_void()
            else:
                default_value = ir.Constant(self._convert_type(node.return_type), 0)
                self.builder.ret(default_value)
        
        # Restaurar estado
        self.builder = old_builder
        self.local_vars = old_vars
        self.current_function = old_func
        self.current_function_ast = old_func_ast
    
    def _generate_statement(self, node: ASTNode):
        if node is None:
            return  # Ignorar statements nulos
        
        if isinstance(node, AssignmentNode):
            self._generate_assignment(node)
        elif isinstance(node, ArrayAssignmentNode):
            self._generate_array_assignment(node)
        elif isinstance(node, ArrayFieldAssignmentNode):
            self._generate_array_field_assignment(node)
        elif isinstance(node, PrintNode):
            self._generate_print(node)
        elif isinstance(node, IfNode):
            self._generate_if(node)
        elif isinstance(node, WhileNode):
            self._generate_while(node)
        elif isinstance(node, ReturnNode):
            self._generate_return(node)
        elif isinstance(node, BreakNode):
            self._generate_break(node)
        elif isinstance(node, StructDefinitionNode):
            # Structs são definições de tipo, não geram código executável
            pass
        elif isinstance(node, StructAssignmentNode):
            self._generate_struct_assignment(node)
        elif isinstance(node, NestedStructAssignmentNode):
            self._generate_nested_struct_assignment(node)
        elif isinstance(node, StructConstructorNode):
            self._generate_struct_constructor(node)
        elif isinstance(node, UseNode):
            # UseNode é processado durante a análise semântica, não gera código
            pass
        else:
            # Expressão simples
            self._generate_expression(node)
    
    def _generate_assignment(self, node: AssignmentNode):
        # Tratar como global apenas se for declaração (tem tipo) E já existir em self.global_vars
        is_declared_global = (node.is_global and node.var_type is not None and node.identifier in self.global_vars)
        if is_declared_global:
            target_type = node.var_type
            gv = self.global_vars[node.identifier]
            # Arrays globais precisam de cópia elemento a elemento
            if isinstance(target_type, ArrayType):
                # Gerar valor como ponteiro para elementos quando possível
                element_ll = self._convert_type(target_type.element_type)
                value = self._generate_expression(node.value, element_ll.as_pointer())
                # Obter ponteiro para primeiro elemento do array global
                zero32 = ir.IntType(32)(0)
                dst_elem_ptr = self.builder.gep(gv, [zero32, zero32], inbounds=True)
                # Copiar elementos
                num = target_type.size or 0
                for i in range(num):
                    idx_const = self.int_type(i)
                    src_ptr = self.builder.gep(value, [idx_const], inbounds=True)
                    src_val = self.builder.load(src_ptr)
                    dst_ptr = self.builder.gep(dst_elem_ptr, [idx_const], inbounds=True)
                    # Ajustar tipo se necessário
                    if src_val.type != dst_ptr.type.pointee:
                        try:
                            if hasattr(src_val.type, 'width') and hasattr(dst_ptr.type.pointee, 'width'):
                                if src_val.type.width < dst_ptr.type.pointee.width:
                                    src_val = self.builder.sext(src_val, dst_ptr.type.pointee)
                                elif src_val.type.width > dst_ptr.type.pointee.width:
                                    src_val = self.builder.trunc(src_val, dst_ptr.type.pointee)
                                else:
                                    src_val = self.builder.bitcast(src_val, dst_ptr.type.pointee)
                            else:
                                src_val = self.builder.bitcast(src_val, dst_ptr.type.pointee)
                        except Exception:
                            pass
                    self.builder.store(src_val, dst_ptr)
                return
            # Strings globais: apenas armazenar ponteiro (ajustando tipo)
            elif isinstance(target_type, StringType) or isinstance(target_type, StrType):
                value = self._generate_expression(node.value)
                if isinstance(value.type, ir.PointerType) and value.type.pointee != self.char_type:
                    value = self.builder.bitcast(value, self.string_type)
                self.builder.store(value, gv)
                return
            # Referências globais: ajustar cast de ponteiro conforme necessário
            elif isinstance(target_type, ReferenceType):
                expected_ptr_ty = self._convert_type(target_type)
                value = self._generate_expression(node.value)
                if isinstance(value.type, ir.PointerType) and value.type != expected_ptr_ty:
                    value = self.builder.bitcast(value, expected_ptr_ty)
                elif isinstance(value.type, ir.IntType) and value.type.width == 64:
                    # null literal para referência
                    value = ir.Constant(expected_ptr_ty, None)
                self.builder.store(value, gv)
                return
            else:
                # Tipos escalares
                expected_ty = self._convert_type(target_type)
                value = self._generate_expression(node.value, expected_ty)
                self.builder.store(value, gv)
                return
        else:
            # Variável local ou reatribuição
            # Configurar tipo de struct atual para geração de valores null
            if node.var_type and isinstance(node.var_type, StructType):
                self.current_struct_type = node.var_type.name
            else:
                self.current_struct_type = None
            
            expected_ty = self._convert_type(node.var_type) if node.var_type is not None else None
            value = self._generate_expression(node.value, expected_ty)
            
            if node.var_type is None:
                # Reatribuição - variável já existe
                # IMPORTANTE: Dentro de uma função, variáveis locais (incluindo parâmetros)
                # sempre têm prioridade sobre variáveis globais para evitar conflitos de escopo
                if node.identifier in self.local_vars:
                    self.builder.store(value, self.local_vars[node.identifier])
                elif node.identifier in self.global_vars:
                    # Reatribuição de global: tratar arrays e strings especificamente
                    gv = self.global_vars[node.identifier]
                    target_type = self.type_map.get(node.identifier)
                    if isinstance(target_type, ArrayType):
                        # Copiar elementos para array global
                        zero32 = ir.IntType(32)(0)
                        dst_elem_ptr = self.builder.gep(gv, [zero32, zero32], inbounds=True)
                        # Se value for ponteiro para elementos, copiar diretamente
                        src_ptr_base = value
                        if not isinstance(value.type, ir.PointerType):
                            # Tentar interpretar como ponteiro para elementos esperados
                            elem_ll = self._convert_type(target_type.element_type)
                            src_ptr_base = self.builder.bitcast(value, elem_ll.as_pointer())
                        num = target_type.size or 0
                        for i in range(num):
                            idx_const = self.int_type(i)
                            src_ptr = self.builder.gep(src_ptr_base, [idx_const], inbounds=True)
                            src_val = self.builder.load(src_ptr)
                            dst_ptr = self.builder.gep(dst_elem_ptr, [idx_const], inbounds=True)
                            if src_val.type != dst_ptr.type.pointee:
                                try:
                                    if hasattr(src_val.type, 'width') and hasattr(dst_ptr.type.pointee, 'width'):
                                        if src_val.type.width < dst_ptr.type.pointee.width:
                                            src_val = self.builder.sext(src_val, dst_ptr.type.pointee)
                                        elif src_val.type.width > dst_ptr.type.pointee.width:
                                            src_val = self.builder.trunc(src_val, dst_ptr.type.pointee)
                                        else:
                                            src_val = self.builder.bitcast(src_val, dst_ptr.type.pointee)
                                    else:
                                        src_val = self.builder.bitcast(src_val, dst_ptr.type.pointee)
                                except Exception:
                                    pass
                            self.builder.store(src_val, dst_ptr)
                    elif isinstance(target_type, (StringType, StrType)):
                        if isinstance(value.type, ir.PointerType) and value.type.pointee != self.char_type:
                            value = self.builder.bitcast(value, self.string_type)
                        self.builder.store(value, gv)
                    elif isinstance(target_type, ReferenceType):
                        expected_ptr_ty = self._convert_type(target_type)
                        if isinstance(value.type, ir.PointerType) and value.type != expected_ptr_ty:
                            value = self.builder.bitcast(value, expected_ptr_ty)
                        elif isinstance(value.type, ir.IntType) and value.type.width == 64:
                            value = ir.Constant(expected_ptr_ty, None)
                        self.builder.store(value, gv)
                    else:
                        self.builder.store(value, gv)
                else:
                    self._semantic_error(f"Variável '{node.identifier}' não foi declarada", node)
            else:
                # Nova variável local
                var_type = self._convert_type(node.var_type)
                alloca = self.builder.alloca(var_type, name=node.identifier)
                self.local_vars[node.identifier] = alloca
                self.type_map[node.identifier] = node.var_type
                

                
                # Armazenar valor
                if isinstance(node.var_type, ArrayType):
                    # Para arrays, precisamos copiar os elementos
                    if node.var_type.size is not None:
                        # Array com tamanho fixo - copiar elementos
                        # Primeiro, obter ponteiro para o primeiro elemento do array destino
                        zero = ir.Constant(ir.IntType(32), 0)
                        dst_array_ptr = self.builder.gep(alloca, [zero, zero], inbounds=True)
                        
                        # Verificar se o valor é um ArrayNode (array literal)
                        if hasattr(node.value, 'elements') and isinstance(node.value.elements, list):
                            # É um array literal, copiar elementos
                            for i in range(min(node.var_type.size, len(node.value.elements))):
                                # Gerar o valor do elemento diretamente
                                elem_value = self._generate_expression(node.value.elements[i])
                                
                                # Armazenar no array destino
                                dst_ptr = self.builder.gep(dst_array_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                                self.builder.store(elem_value, dst_ptr)
                        else:
                            # É um ponteiro para array, copiar elementos
                            for i in range(node.var_type.size):
                                # Carregar o valor do array fonte
                                src_ptr = self.builder.gep(value, [ir.Constant(self.int_type, i)], inbounds=True)
                                src_val = self.builder.load(src_ptr)
                                
                                # Armazenar no array destino
                                dst_ptr = self.builder.gep(dst_array_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                                self.builder.store(src_val, dst_ptr)
                    else:
                        # Array dinâmico - armazenar ponteiro
                        if isinstance(value.type, ir.PointerType) and isinstance(var_type, ir.PointerType):
                            if value.type != var_type:
                                value = self.builder.bitcast(value, var_type)
                        self.builder.store(value, alloca)
                elif isinstance(node.var_type, ReferenceType):
                    # Para referências, fazer cast se necessário
                    if isinstance(value.type, ir.IntType) and value.type.width == 64:
                        # Se o valor é null (i64), converter para ponteiro nulo
                        null_ptr = ir.Constant(var_type, None)
                        self.builder.store(null_ptr, alloca)
                    elif (isinstance(value.type, ir.PointerType) and
                          isinstance(var_type, ir.PointerType) and
                          value.type != var_type):
                        # Ex.: i8* -> {struct}*
                        value = self.builder.bitcast(value, var_type)
                        self.builder.store(value, alloca)
                    elif (isinstance(value.type, ir.PointerType) and 
                        isinstance(value.type.pointee, ir.LiteralStructType) and
                        isinstance(var_type, ir.PointerType) and
                        var_type.pointee == ir.IntType(8)):
                        # Cast de ponteiro para struct para ponteiro para void
                        value = self.builder.bitcast(value, var_type)
                        self.builder.store(value, alloca)
                    else:
                        # Para outros casos, armazenar diretamente
                        self.builder.store(value, alloca)
                elif isinstance(node.var_type, StructType):
                    # Para structs, tratar null especialmente
                    if isinstance(value.type, ir.IntType) and value.type.width == 64:
                        # Se o valor é null (i64), converter para ponteiro nulo do tipo correto
                        null_ptr = ir.Constant(var_type, None)
                        self.builder.store(null_ptr, alloca)
                    else:
                        # Para outros valores, armazenar diretamente
                        try:
                            self.builder.store(value, alloca)
                        except TypeError:
                            # Se falhar, tentar fazer cast
                            if isinstance(value.type, ir.PointerType) and isinstance(var_type, ir.PointerType):
                                value = self.builder.bitcast(value, var_type)
                            self.builder.store(value, alloca)
                else:
                    # Para tipos simples
                    self.builder.store(value, alloca)
    
    def _generate_array_assignment(self, node: ArrayAssignmentNode):
        # Verificar se é um acesso a campo de struct (ex: arr.elementos[i])
        if '.' in node.array_name:
            # Dividir o nome: "arr.elementos" -> ["arr", "elementos"]
            parts = node.array_name.split('.')
            struct_name = parts[0]
            field_name = parts[1]
            
            # Procurar a variável struct primeiro localmente, depois globalmente
            if struct_name in self.local_vars:
                struct_ptr = self.local_vars[struct_name]
                # Verificar se é um parâmetro de função (ponteiro para ponteiro)
                if isinstance(struct_ptr, ir.Argument):
                    # É um parâmetro de função, precisa dereferenciar
                    struct_ptr = self.builder.load(struct_ptr)
                elif (struct_name in self.type_map and 
                      isinstance(self.type_map[struct_name], ReferenceType)):
                    # É uma referência, precisa dereferenciar
                    struct_ptr = self.builder.load(struct_ptr)
                # Se for um alloca (variável local), fazer load
                import llvmlite.ir.instructions
                if isinstance(struct_ptr, llvmlite.ir.instructions.AllocaInstr):
                    struct_ptr = self.builder.load(struct_ptr)
            elif struct_name in self.global_vars:
                struct_ptr = self.global_vars[struct_name]
                # Se for uma GlobalVariable cujo pointee é ponteiro (ex.: Bag**), carregar para obter Bag*
                from llvmlite import ir as _ir
                if isinstance(struct_ptr, _ir.GlobalVariable):
                    if isinstance(struct_ptr.type, _ir.PointerType) and isinstance(struct_ptr.type.pointee, _ir.PointerType):
                        struct_ptr = self.builder.load(struct_ptr)
            else:
                self._semantic_error(f"Variável '{struct_name}' não encontrada", node)
            
            # Determinar o tipo de struct baseado na variável
            struct_type_name = None
            if struct_name in self.type_map:
                var_type = self.type_map[struct_name]
                if isinstance(var_type, StructType):
                    struct_type_name = var_type.name
                elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                    struct_type_name = var_type.target_type.name
            
            if not struct_type_name or struct_type_name not in self.struct_fields:
                raise NameError(f"Não foi possível determinar o tipo da variável '{struct_name}'")
            
            # Verificar se o campo existe
            if field_name not in self.struct_fields[struct_type_name]:
                self._semantic_error(f"Campo '{field_name}' não encontrado em struct '{struct_type_name}'", node)
            
            # Fazer cast do ponteiro para o tipo correto do struct
            if struct_type_name in self.struct_types:
                struct_type = self.struct_types[struct_type_name]
                struct_ptr = self.builder.bitcast(struct_ptr, struct_type.as_pointer())
            
            # Obter o índice do campo
            field_index = self.struct_fields[struct_type_name][field_name]
            
            # Acessar o campo do struct
            field_ptr = self.builder.gep(struct_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
            
            # O campo é um array, obter ponteiro para o primeiro elemento
            if isinstance(field_ptr.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.ArrayType):
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(field_ptr, [zero, zero], inbounds=True)
            else:
                array_ptr = field_ptr
            
            # Continuar com a lógica normal de atribuição de array
            index = self._generate_expression(node.index)
            value = self._generate_expression(node.value)
            
            # Calcular endereço do elemento
            elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
            
            # Armazenar valor
            self.builder.store(value, elem_ptr)
            return
        
        # Caso normal: array simples
        # Procurar variável primeiro localmente, depois globalmente
        if node.array_name in self.local_vars:
            var = self.local_vars[node.array_name]
        elif node.array_name in self.global_vars:
            var = self.global_vars[node.array_name]
        else:
            raise NameError(f"Array '{node.array_name}' não definido")
        
        index = self._generate_expression(node.index)
        value = self._generate_expression(node.value)
        
        # Se a variável já é um ponteiro (parâmetro de função), usar diretamente
        if isinstance(var, ir.Argument) and isinstance(var.type, ir.PointerType):
            array_ptr = var
        elif isinstance(var, ir.GlobalVariable) and isinstance(var.type.pointee, ir.ArrayType):
            # Variável global que é array
            zero = ir.Constant(ir.IntType(32), 0)
            array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
        else:
            # Para arrays locais, não fazer load, usar diretamente o ponteiro
            if isinstance(var.type, ir.PointerType) and isinstance(var.type.pointee, ir.ArrayType):
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
            else:
                # Verificar se é uma referência (precisa fazer load primeiro)
                if (node.array_name in self.type_map and 
                    isinstance(self.type_map[node.array_name], ReferenceType)):
                    # É uma referência, fazer load para obter o ponteiro real
                    array_ptr = self.builder.load(var)
                    # Se o resultado é um ponteiro para array, obter ponteiro para primeiro elemento
                    if isinstance(array_ptr.type, ir.PointerType) and isinstance(array_ptr.type.pointee, ir.ArrayType):
                        zero = ir.Constant(ir.IntType(32), 0)
                        array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)
                else:
                    array_ptr = self.builder.load(var)
                    if isinstance(array_ptr.type, ir.ArrayType):
                        zero = ir.Constant(ir.IntType(32), 0)
                        array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)
        
        # Verificar se array_ptr é um ponteiro válido
        if not isinstance(array_ptr.type, ir.PointerType):
            # Se não é ponteiro, fazer cast para ponteiro
            array_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
        
        # Calcular endereço do elemento
        elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
        
        # Armazenar valor
        self.builder.store(value, elem_ptr)
    
    def _generate_array_field_assignment(self, node: ArrayFieldAssignmentNode):
        """Gera código para atribuição de campo de struct em array: array[index].campo = valor"""
        # Primeiro, obter o ponteiro para o array
        if node.array_name in self.local_vars:
            array_var = self.local_vars[node.array_name]
        elif node.array_name in self.global_vars:
            array_var = self.global_vars[node.array_name]
        else:
            self._semantic_error(f"Array '{node.array_name}' não encontrado", node)
        
        # Gerar o índice
        index = self._generate_expression(node.index)
        
        # Gerar o valor para atribuição
        value = self._generate_expression(node.value)
        
        # Obter o ponteiro para o elemento do array
        array_ptr = None
        if isinstance(array_var, ir.Argument) and isinstance(array_var.type, ir.PointerType):
            # Parâmetro de função
            array_ptr = array_var
        elif isinstance(array_var, ir.GlobalVariable) and isinstance(array_var.type.pointee, ir.ArrayType):
            # Array global
            zero = ir.Constant(ir.IntType(32), 0)
            array_ptr = self.builder.gep(array_var, [zero, zero], inbounds=True)
        elif isinstance(array_var.type, ir.PointerType) and isinstance(array_var.type.pointee, ir.ArrayType):
            # Array local
            zero = ir.Constant(ir.IntType(32), 0)
            array_ptr = self.builder.gep(array_var, [zero, zero], inbounds=True)
        else:
            # Array de ponteiros ou outro caso
            array_ptr = self.builder.load(array_var)
        
        # Calcular o ponteiro para o elemento específico do array
        elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
        
        # Se o elemento é um ponteiro para struct, fazer load para obter o struct
        if isinstance(elem_ptr.type, ir.PointerType) and isinstance(elem_ptr.type.pointee, ir.PointerType):
            struct_ptr = self.builder.load(elem_ptr)
        else:
            struct_ptr = elem_ptr
        
        # Determinar o tipo do struct baseado no tipo do array
        struct_type_name = None
        if node.array_name in self.type_map:
            array_type = self.type_map[node.array_name]
            if isinstance(array_type, ArrayType) and isinstance(array_type.element_type, StructType):
                struct_type_name = array_type.element_type.name
        
        if not struct_type_name or struct_type_name not in self.struct_fields:
            self._semantic_error(f"Não foi possível determinar o tipo do struct no array '{node.array_name}'", node)
        
        # Navegar pelos campos (pode haver aninhamento como array[0].pessoa.nome)
        current_ptr = struct_ptr
        current_struct_type = struct_type_name
        
        for i, field_name in enumerate(node.field_path):
            if current_struct_type not in self.struct_fields:
                self._semantic_error(f"Tipo de struct '{current_struct_type}' não encontrado", node)
            
            if field_name not in self.struct_fields[current_struct_type]:
                self._semantic_error(f"Campo '{field_name}' não encontrado em struct '{current_struct_type}'", node)
            
            # Obter o índice do campo
            field_index = self.struct_fields[current_struct_type][field_name]
            
            # Se não é o último campo, navegar para o próximo nível
            if i < len(node.field_path) - 1:
                # Acessar o campo e continuar navegando
                field_ptr = self.builder.gep(current_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                
                # Determinar o tipo do próximo struct
                # TODO: Implementar lógica para structs aninhados
                # Por enquanto, assumimos apenas um nível
                current_ptr = field_ptr
                current_struct_type = None  # Precisaria determinar o tipo do campo
            else:
                # É o último campo, fazer a atribuição
                field_ptr = self.builder.gep(current_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                self.builder.store(value, field_ptr)
    
    def _generate_print(self, node: PrintNode):
        # Verificar se estamos imprimindo uma concatenação
        if isinstance(node.expression, ConcatNode):
            # Para concatenações, gerar o código e imprimir como string
            value = self._generate_expression(node.expression)
            self._print_string(value)
            return
        
        value = self._generate_expression(node.expression)
        
        # Verificar se estamos imprimindo um array diretamente
        if isinstance(node.expression, IdentifierNode):
            var_name = node.expression.name
            if var_name in self.type_map and isinstance(self.type_map[var_name], ArrayType):
                # É um array, vamos imprimir especialmente
                self._print_array(var_name)
                return
        # Verificar se estamos imprimindo um campo de struct que é um array
        elif isinstance(node.expression, StructAccessNode):
            struct_name = node.expression.struct_name
            field_name = node.expression.field_name
            
            # Determinar o tipo de struct baseado na variável
            struct_type_name = None
            if struct_name in self.type_map:
                var_type = self.type_map[struct_name]
                if isinstance(var_type, StructType):
                    struct_type_name = var_type.name
                elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                    struct_type_name = var_type.target_type.name
            
            # Verificar se o campo é um array
            if (struct_type_name and struct_type_name in self.struct_fields and 
                field_name in self.struct_fields[struct_type_name]):
                # Obter o índice do campo
                field_index = self.struct_fields[struct_type_name][field_name]
                if struct_type_name in self.struct_types:
                    struct_type = self.struct_types[struct_type_name]
                    if field_index < len(struct_type.elements):
                        field_llvm_type = struct_type.elements[field_index]
                        # Se o campo é um ponteiro (array), verificar se não é string primeiro
                        if isinstance(field_llvm_type, ir.PointerType):
                            # Verificar se é uma string (i8*) ou um array real
                            if field_llvm_type.pointee == self.char_type:
                                # É uma string, usar o comportamento normal de print
                                pass
                            else:
                                # É um array real, usar a função especializada
                                self._print_struct_array_field(struct_name, field_name, field_llvm_type)
                                return
        
        # Determinar formato baseado no tipo
        if value.type == self.bool_type:
            fmt_str = "%s\n\0"  # Usar %s para "true"/"false"
        elif isinstance(value.type, ir.IntType):
            if value.type.width == 8:  # i8 (char)
                fmt_str = "%c\n\0"
            else:
                fmt_str = "%lld\n\0"
        elif isinstance(value.type, ir.DoubleType):
            fmt_str = "%f\n\0"
        elif isinstance(value.type, ir.PointerType) and value.type.pointee == self.char_type:
            fmt_str = "%s\n\0"
        else:
            fmt_str = "%p\n\0"  # Ponteiro genérico
            
        # Criar string global com nome único
        fmt_name = f"fmt_str_{len(self.module.globals)}"
        fmt_bytes = fmt_str.encode('utf8')
        fmt = ir.GlobalVariable(self.module, ir.ArrayType(ir.IntType(8), len(fmt_bytes)), name=fmt_name)
        fmt.linkage = 'internal'
        fmt.global_constant = True
        fmt.initializer = ir.Constant(ir.ArrayType(ir.IntType(8), len(fmt_bytes)),
                                     bytearray(fmt_bytes))
        
        # Obter ponteiro para o início do array usando GEP
        zero = ir.Constant(ir.IntType(32), 0)
        fmt_ptr = self.builder.gep(fmt, [zero, zero], inbounds=True)
        
        # Tratamento especial para booleanos
        if value.type == self.bool_type:
            # Criar strings "true" e "false"
            true_str = "true\0"
            false_str = "false\0"
            true_bytes = true_str.encode('utf8')
            false_bytes = false_str.encode('utf8')
            
            true_type = ir.ArrayType(self.char_type, len(true_bytes))
            false_type = ir.ArrayType(self.char_type, len(false_bytes))
            
            true_name = f"true_str_{len(self.module.globals)}"
            false_name = f"false_str_{len(self.module.globals)}"
            
            true_global = ir.GlobalVariable(self.module, true_type, name=true_name)
            true_global.linkage = 'internal'
            true_global.global_constant = True
            true_global.initializer = ir.Constant(true_type, bytearray(true_bytes))
            
            false_global = ir.GlobalVariable(self.module, false_type, name=false_name)
            false_global.linkage = 'internal'
            false_global.global_constant = True
            false_global.initializer = ir.Constant(false_type, bytearray(false_bytes))
            
            true_ptr = self.builder.gep(true_global, [zero, zero], inbounds=True)
            false_ptr = self.builder.gep(false_global, [zero, zero], inbounds=True)
            
            # Usar select para escolher entre "true" e "false"
            bool_str = self.builder.select(value, true_ptr, false_ptr)
            self.builder.call(self.printf, [fmt_ptr, bool_str])
        else:
            # Chamar printf
            self.builder.call(self.printf, [fmt_ptr, value])
    
    def _print_struct_array_field(self, struct_name: str, field_name: str, field_type: ir.Type):
        """Imprime um campo de array de um struct"""
        # Obter ponteiro do struct
        if struct_name in self.local_vars:
            struct_ptr = self.local_vars[struct_name]
            # SEMPRE fazer load se for variável local (alloca)
            import llvmlite.ir.instructions
            if isinstance(struct_ptr, llvmlite.ir.instructions.AllocaInstr):
                struct_ptr = self.builder.load(struct_ptr)
        elif struct_name in self.global_vars:
            struct_ptr = self.global_vars[struct_name]
        else:
            return
        
        # Determinar o tipo de struct baseado na variável
        struct_type_name = None
        if struct_name in self.type_map:
            var_type = self.type_map[struct_name]
            if isinstance(var_type, StructType):
                struct_type_name = var_type.name
            elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                struct_type_name = var_type.target_type.name
        
        if not struct_type_name or struct_type_name not in self.struct_fields:
            return
        
        # Verificar se o campo é uma string (não um array)
        if struct_type_name in self.struct_types:
            # Obter o tipo original do campo no struct
            struct_type = self.struct_types[struct_type_name]
            field_index = self.struct_fields[struct_type_name][field_name]
            
            # Verificar se o campo original era StringType
            original_field_type = None
            if struct_type_name in self.struct_types:
                # Encontrar o tipo original do campo
                for field_name_orig, field_type_orig in var_type.fields.items():
                    if field_name_orig == field_name:
                        original_field_type = field_type_orig
                        break
            
            # Se é uma string, imprimir como string
            if isinstance(original_field_type, StringType) or isinstance(original_field_type, StrType):
                # Acessar o campo usando getelementptr
                field_ptr = self.builder.gep(struct_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                
                # Carregar o ponteiro da string
                string_ptr = self.builder.load(field_ptr)
                
                # Imprimir como string
                self._print_string(string_ptr)
                return
        
        # Obter o índice do campo
        field_index = self.struct_fields[struct_type_name][field_name]
        
        # Acessar o campo usando getelementptr
        field_ptr = self.builder.gep(struct_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
        
        # Carregar o ponteiro do array
        array_ptr = self.builder.load(field_ptr)
        
        # Imprimir o array
        self._print_array_from_ptr(array_ptr, field_type)
    
    def _print_string(self, string_ptr: ir.Value):
        """Imprime uma string"""
        # Debug: log quando esta função é chamada
        print(f"DEBUG: _print_string chamada com string_ptr: {string_ptr}")
        
        # Formato para string
        fmt_str = "%s\n\0"
        fmt_bytes = fmt_str.encode('utf8')
        fmt_type = ir.ArrayType(self.char_type, len(fmt_bytes))
        fmt_name = f"fmt_str_{len(self.module.globals)}"
        fmt_global = ir.GlobalVariable(self.module, fmt_type, name=fmt_name)
        fmt_global.linkage = 'internal'
        fmt_global.global_constant = True
        fmt_global.initializer = ir.Constant(fmt_type, bytearray(fmt_bytes))
        fmt_ptr = self.builder.gep(fmt_global, [ir.Constant(self.int_type, 0), ir.Constant(self.int_type, 0)], inbounds=True)
        
        # Imprimir a string
        self.builder.call(self.printf, [fmt_ptr, string_ptr])
    
    def _print_array_from_ptr(self, array_ptr: ir.Value, array_type: ir.Type, array_size: int = None):
        """Imprime um array a partir de um ponteiro"""
        # Determinar o tipo de elemento e tamanho baseado no tipo do ponteiro
        if isinstance(array_type, ir.PointerType):
            elem_type = array_type.pointee
            # Usar o tamanho fornecido ou determinar baseado no tipo do elemento
            if array_size is None:
                if elem_type == self.int_type:
                    array_size = 4  # Para arrays de int
                elif elem_type == self.bool_type:
                    array_size = 5  # Para arrays de bool
                else:
                    array_size = 3  # Padrão
        else:
            return
        
        zero = ir.Constant(self.int_type, 0)

        # String "["
        bracket_open_str = "[\0"
        bracket_open_bytes = bracket_open_str.encode('utf8')
        bracket_open_type = ir.ArrayType(self.char_type, len(bracket_open_bytes))
        bracket_open_name = f"bracket_open_{len(self.module.globals)}"
        bracket_open_global = ir.GlobalVariable(self.module, bracket_open_type, name=bracket_open_name)
        bracket_open_global.linkage = 'internal'
        bracket_open_global.global_constant = True
        bracket_open_global.initializer = ir.Constant(bracket_open_type, bytearray(bracket_open_bytes))
        bracket_open_ptr = self.builder.gep(bracket_open_global, [zero, zero], inbounds=True)

        # String "%s"
        fmt_s_str = "%s\0"
        fmt_s_bytes = fmt_s_str.encode('utf8')
        fmt_s_type = ir.ArrayType(self.char_type, len(fmt_s_bytes))
        fmt_s_name = f"fmt_s_{len(self.module.globals)}"
        fmt_s_global = ir.GlobalVariable(self.module, fmt_s_type, name=fmt_s_name)
        fmt_s_global.linkage = 'internal'
        fmt_s_global.global_constant = True
        fmt_s_global.initializer = ir.Constant(fmt_s_type, bytearray(fmt_s_bytes))
        fmt_s_ptr = self.builder.gep(fmt_s_global, [zero, zero], inbounds=True)

        # Imprimir "["
        self.builder.call(self.printf, [fmt_s_ptr, bracket_open_ptr])

        # String de formatação para elementos
        if elem_type == self.bool_type:
            fmt_elem_str = "%s\0"  # Usar %s para "true"/"false"
        elif isinstance(elem_type, ir.IntType):
            fmt_elem_str = "%lld\0"
        elif isinstance(elem_type, ir.DoubleType):
            fmt_elem_str = "%f\0"
        elif isinstance(elem_type, ir.PointerType) and elem_type.pointee == self.char_type:
            fmt_elem_str = "%s\0"  # Usar %s para strings
        elif isinstance(elem_type, ir.PointerType) and isinstance(elem_type.pointee, ir.PointerType) and elem_type.pointee.pointee == self.char_type:
            fmt_elem_str = "%s\0"  # Usar %s para arrays de strings
        elif elem_type == self.string_type:
            fmt_elem_str = "%s\0"  # Usar %s para strings (quando elem_type é string_type diretamente)
        else:
            fmt_elem_str = "%p\0"
        fmt_elem_bytes = fmt_elem_str.encode('utf8')
        fmt_elem_type = ir.ArrayType(self.char_type, len(fmt_elem_bytes))
        fmt_elem_name = f"fmt_elem_{len(self.module.globals)}"
        fmt_elem_global = ir.GlobalVariable(self.module, fmt_elem_type, name=fmt_elem_name)
        fmt_elem_global.linkage = 'internal'
        fmt_elem_global.global_constant = True
        fmt_elem_global.initializer = ir.Constant(fmt_elem_type, bytearray(fmt_elem_bytes))
        fmt_elem_ptr = self.builder.gep(fmt_elem_global, [zero, zero], inbounds=True)

        # String ", "
        comma_str = ", \0"
        comma_bytes = comma_str.encode('utf8')
        comma_type = ir.ArrayType(self.char_type, len(comma_bytes))
        comma_name = f"comma_{len(self.module.globals)}"
        comma_global = ir.GlobalVariable(self.module, comma_type, name=comma_name)
        comma_global.linkage = 'internal'
        comma_global.global_constant = True
        comma_global.initializer = ir.Constant(comma_type, bytearray(comma_bytes))
        comma_ptr = self.builder.gep(comma_global, [zero, zero], inbounds=True)

        # Imprimir cada elemento
        for i in range(array_size):
            if i > 0:
                self.builder.call(self.printf, [fmt_s_ptr, comma_ptr])
            elem_ptr = self.builder.gep(array_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
            elem_value = self.builder.load(elem_ptr)
            
            # Tratamento especial para booleanos
            if elem_type == self.bool_type:
                # Criar strings "true" e "false"
                true_str = "true\0"
                false_str = "false\0"
                true_bytes = true_str.encode('utf8')
                false_bytes = false_str.encode('utf8')
                
                true_type = ir.ArrayType(self.char_type, len(true_bytes))
                false_type = ir.ArrayType(self.char_type, len(false_bytes))
                
                true_name = f"true_str_{len(self.module.globals)}"
                false_name = f"false_str_{len(self.module.globals)}"
                
                true_global = ir.GlobalVariable(self.module, true_type, name=true_name)
                true_global.linkage = 'internal'
                true_global.global_constant = True
                true_global.initializer = ir.Constant(true_type, bytearray(true_bytes))
                
                false_global = ir.GlobalVariable(self.module, false_type, name=false_name)
                false_global.linkage = 'internal'
                false_global.global_constant = True
                false_global.initializer = ir.Constant(false_type, bytearray(false_bytes))
                
                true_ptr = self.builder.gep(true_global, [zero, zero], inbounds=True)
                false_ptr = self.builder.gep(false_global, [zero, zero], inbounds=True)
                
                # Usar select para escolher entre "true" e "false"
                bool_str = self.builder.select(elem_value, true_ptr, false_ptr)
                self.builder.call(self.printf, [fmt_elem_ptr, bool_str])
            else:
                self.builder.call(self.printf, [fmt_elem_ptr, elem_value])

        # String "]\n"
        bracket_close_str = "]\n\0"
        bracket_close_bytes = bracket_close_str.encode('utf8')
        bracket_close_type = ir.ArrayType(self.char_type, len(bracket_close_bytes))
        bracket_close_name = f"bracket_close_{len(self.module.globals)}"
        bracket_close_global = ir.GlobalVariable(self.module, bracket_close_type, name=bracket_close_name)
        bracket_close_global.linkage = 'internal'
        bracket_close_global.global_constant = True
        bracket_close_global.initializer = ir.Constant(bracket_close_type, bytearray(bracket_close_bytes))
        bracket_close_ptr = self.builder.gep(bracket_close_global, [zero, zero], inbounds=True)
        self.builder.call(self.printf, [fmt_s_ptr, bracket_close_ptr])
    
    def _print_array(self, array_name: str):
        """Imprime um array de forma formatada (int, float ou string)"""
        # Obter informações sobre o array
        array_type = self.type_map.get(array_name)
        if not isinstance(array_type, ArrayType):
            return
        elem_type = array_type.element_type
        array_size = array_type.size if array_type.size else 5

        # Obter ponteiro do array
        if array_name in self.local_vars:
            var = self.local_vars[array_name]
        elif array_name in self.global_vars:
            var = self.global_vars[array_name]
        else:
            return

        # Carregar o ponteiro do array
        if isinstance(var, ir.Argument) and isinstance(var.type, ir.PointerType):
            array_ptr = var
        elif isinstance(var, ir.GlobalVariable) and isinstance(var.type.pointee, ir.ArrayType):
            array_ptr = self.builder.gep(var, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), 0)], inbounds=True)
        else:
            array_ptr = self.builder.load(var)
        
        # Converter o tipo para LLVM
        llvm_elem_type = self._convert_type(elem_type)
        
        # Para arrays de strings, precisamos passar o tipo correto
        if isinstance(elem_type, StringType) or isinstance(elem_type, StrType):
            # Para strings, o tipo LLVM é um ponteiro para char
            array_llvm_type = self.string_type.as_pointer()
        else:
            if isinstance(llvm_elem_type, ir.PointerType):
                array_llvm_type = llvm_elem_type
            else:
                array_llvm_type = llvm_elem_type.as_pointer()
        
        # Imprimir usando o método existente com o tamanho correto
        self._print_array_from_ptr(array_ptr, array_llvm_type, array_size)
    
    def _generate_if(self, node: IfNode):
        # Gerar condição
        condition = self._generate_expression(node.condition)
        
        # Criar blocos
        then_block = self.current_function.append_basic_block(name="then")
        end_block = self.current_function.append_basic_block(name="endif")
        
        if node.else_branch:
            else_block = self.current_function.append_basic_block(name="else")
            self.builder.cbranch(condition, then_block, else_block)
        else:
            self.builder.cbranch(condition, then_block, end_block)
        
        # Gerar código do then
        self.builder.position_at_end(then_block)
        for stmt in node.then_branch:
            self._generate_statement(stmt)
        if not self.builder.block.is_terminated:
            self.builder.branch(end_block)
        
        # Gerar código do else se existir
        if node.else_branch:
            self.builder.position_at_end(else_block)
            for stmt in node.else_branch:
                self._generate_statement(stmt)
            if not self.builder.block.is_terminated:
                self.builder.branch(end_block)
        
        # Continuar no end block
        self.builder.position_at_end(end_block)
    
    def _generate_while(self, node: WhileNode):
        # Criar blocos
        cond_block = self.current_function.append_basic_block(name="while_cond")
        body_block = self.current_function.append_basic_block(name="while_body")
        end_block = self.current_function.append_basic_block(name="while_end")
        
        # Salvar break target anterior e definir novo
        old_break_target = getattr(self, 'break_target', None)
        self.break_target = end_block
        
        # Ir para bloco de condição
        self.builder.branch(cond_block)
        
        # Gerar condição
        self.builder.position_at_end(cond_block)
        condition = self._generate_expression(node.condition)
        self.builder.cbranch(condition, body_block, end_block)
        
        # Gerar corpo do loop
        self.builder.position_at_end(body_block)
        for stmt in node.body:
            self._generate_statement(stmt)
        if not self.builder.block.is_terminated:
            self.builder.branch(cond_block)
        
        # Restaurar break target anterior
        self.break_target = old_break_target
        
        # Continuar após o loop
        self.builder.position_at_end(end_block)
    
    def _generate_return(self, node: ReturnNode):
        if node.value:
            value = self._generate_expression(node.value)
            self.builder.ret(value)
        else:
            if isinstance(self.current_function.return_value.type, ir.VoidType):
                self.builder.ret_void()
            else:
                self.builder.ret(ir.Constant(self.int_type, 0))
    
    def _generate_break(self, node: BreakNode):
        """Gerar código para a keyword break"""
        if hasattr(self, 'break_target') and self.break_target:
            self.builder.branch(self.break_target)
        else:
            self._semantic_error("'break' usado fora de um loop", node)
    
    def _convert_array_args_for_function_call(self, func: ir.Function, args: List[ir.Value]) -> List[ir.Value]:
        """Converte arrays estáticos para ponteiros quando necessário para chamadas de função"""
        converted_args = []
        for i, (arg, param_type) in enumerate(zip(args, func.args)):
            # Se o argumento é um ponteiro para array estático e o parâmetro espera ponteiro para elemento
            if (isinstance(arg.type, ir.PointerType) and
                isinstance(arg.type.pointee, ir.ArrayType) and
                isinstance(param_type.type, ir.PointerType) and
                arg.type.pointee.element == param_type.type.pointee):
                # GEP [0,0] para obter ponteiro para o primeiro elemento
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(arg, [zero, zero], inbounds=True)
                converted_args.append(array_ptr)
            # Se o argumento é um array estático (valor), mas o parâmetro espera ponteiro
            elif (isinstance(arg.type, ir.ArrayType) and 
                  isinstance(param_type.type, ir.PointerType) and
                  arg.type.element == param_type.type.pointee):
                # Não é o ideal, mas mantém compatibilidade para casos antigos
                element_ptr_type = ir.PointerType(arg.type.element)
                array_ptr = self.builder.bitcast(arg, element_ptr_type)
                converted_args.append(array_ptr)
            # Se o argumento já é um ponteiro para elemento (como arrays locais após GEP)
            elif (isinstance(arg.type, ir.PointerType) and
                  isinstance(param_type.type, ir.PointerType) and
                  arg.type.pointee == param_type.type.pointee):
                # Já é o tipo correto, usar diretamente
                converted_args.append(arg)
            # Tratamento especial para arrays de structs: ponteiro para array de ponteiros -> ponteiro para ponteiro
            elif (isinstance(arg.type, ir.PointerType) and
                  isinstance(arg.type.pointee, ir.ArrayType) and
                  isinstance(arg.type.pointee.element, ir.PointerType) and
                  isinstance(param_type.type, ir.PointerType)):
                # Array de structs: obter ponteiro para o primeiro elemento do array
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(arg, [zero, zero], inbounds=True)
                converted_args.append(array_ptr)
            # Caso específico para arrays de structs que foram declarados sem tamanho
            elif (isinstance(arg.type, ir.PointerType) and
                  isinstance(arg.type.pointee, ir.PointerType) and
                  isinstance(param_type.type, ir.PointerType) and
                  arg.type == param_type.type):
                # Já é o tipo correto (ponteiro para ponteiro), usar diretamente  
                converted_args.append(arg)
            else:
                converted_args.append(arg)
        return converted_args

    def _generate_fstring(self, node: FStringNode) -> ir.Value:
        """Gera código LLVM para uma f-string, concatenando todas as partes"""
        if not node.parts:
            # F-string vazia - retornar string vazia
            return self._create_string_constant("")
        
        # Primeiro, gerar código para todas as partes
        part_values = []
        
        for part in node.parts:
            if isinstance(part, str):
                # Parte literal - criar string constante
                part_values.append(self._create_string_constant(part))
            else:
                # Parte de expressão - gerar e converter para string
                expr_value = self._generate_expression(part)
                # Verificar se há especificador de formato
                format_spec = getattr(part, 'format_spec', None)
                string_value = self._convert_to_string(expr_value, format_spec)
                part_values.append(string_value)
        
        # Se há apenas uma parte, retornar diretamente
        if len(part_values) == 1:
            return part_values[0]
        
        # Concatenar todas as partes usando strcat
        result = part_values[0]
        for i in range(1, len(part_values)):
            result = self._concatenate_strings(result, part_values[i])
        
        return result
    
    def _create_string_constant(self, value: str) -> ir.Value:
        """Cria uma string constante"""
        string_value = value + '\0'  # Adicionar null terminator
        string_bytes = string_value.encode('utf8')
        str_type = ir.ArrayType(self.char_type, len(string_bytes))
        str_name = f"str_{len(self.module.globals)}"
        
        str_global = ir.GlobalVariable(self.module, str_type, name=str_name)
        str_global.linkage = 'internal'
        str_global.global_constant = True
        str_global.initializer = ir.Constant(str_type, bytearray(string_bytes))
        
        # Retornar ponteiro para a string
        zero = ir.Constant(ir.IntType(32), 0)
        return self.builder.gep(str_global, [zero, zero], inbounds=True)
    
    def _convert_to_string(self, value: ir.Value, format_spec: str = None) -> ir.Value:
        """Converte um valor para string com especificador de formato opcional"""
        if value.type == self.char_type.as_pointer():
            # Já é uma string
            return value
        elif value.type == self.int_type:
            # Converter int para string usando sprintf
            return self._int_to_string(value, format_spec)
        elif value.type == self.float_type:
            # Converter float para string usando sprintf
            return self._float_to_string(value, format_spec)
        elif value.type == self.bool_type:
            # Converter bool para string
            return self._bool_to_string(value)
        else:
            # Tipo não suportado - retornar string vazia
            return self._create_string_constant("")
    
    def _int_to_string(self, value: ir.Value, format_spec: str = None) -> ir.Value:
        """Converte um inteiro para string usando sprintf"""
        # Alocar buffer para a string (32 bytes devem ser suficientes para int)
        buffer_size = 32
        buffer_type = ir.ArrayType(self.char_type, buffer_size)
        buffer = self.builder.alloca(buffer_type)
        
        # Determinar format string baseado no especificador
        if format_spec:
            if format_spec.startswith('0') and format_spec[1:].isdigit():
                # Formato com zeros à esquerda: {num:05d} -> "%05d"
                format_string = f"%{format_spec}d"
            elif format_spec.isdigit():
                # Formato com largura mínima: {num:5d} -> "%5d"
                format_string = f"%{format_spec}d"
            elif format_spec in ['x', 'X']:
                # Formato hexadecimal: {num:x} -> "%x"
                format_string = f"%{format_spec}"
            elif format_spec == 'o':
                # Formato octal: {num:o} -> "%o"
                format_string = "%o"
            else:
                # Formato desconhecido, usar padrão
                format_string = "%d"
        else:
            format_string = "%d"
        
        format_str = self._create_string_constant(format_string)
        
        # Chamar sprintf
        zero = ir.Constant(ir.IntType(32), 0)
        buffer_ptr = self.builder.gep(buffer, [zero, zero], inbounds=True)
        
        # Verificar se sprintf existe, se não, criar declaração
        if 'sprintf' not in self.module.globals:
            sprintf_type = ir.FunctionType(self.int_type, [self.char_type.as_pointer(), self.char_type.as_pointer()], var_arg=True)
            sprintf_func = ir.Function(self.module, sprintf_type, name='sprintf')
        else:
            sprintf_func = self.module.globals['sprintf']
        
        self.builder.call(sprintf_func, [buffer_ptr, format_str, value])
        return buffer_ptr
    
    def _float_to_string(self, value: ir.Value, format_spec: str = None) -> ir.Value:
        """Converte um float para string usando sprintf"""
        # Alocar buffer para a string (32 bytes devem ser suficientes para float)
        buffer_size = 32
        buffer_type = ir.ArrayType(self.char_type, buffer_size)
        buffer = self.builder.alloca(buffer_type)
        
        # Determinar format string baseado no especificador
        if format_spec:
            if format_spec.endswith('f') and '.' in format_spec:
                # Formato com precisão decimal: {num:.2f} -> "%.2f"
                format_string = f"%{format_spec}"
            elif format_spec.endswith('f'):
                # Formato float simples: {num:f} -> "%f"
                format_string = "%f"
            elif format_spec.endswith('e') or format_spec.endswith('E'):
                # Formato científico: {num:.2e} -> "%.2e"
                if '.' in format_spec:
                    format_string = f"%{format_spec}"
                else:
                    format_string = f"%{format_spec}"
            elif format_spec.endswith('g') or format_spec.endswith('G'):
                # Formato geral: {num:.2g} -> "%.2g"
                if '.' in format_spec:
                    format_string = f"%{format_spec}"
                else:
                    format_string = f"%{format_spec}"
            elif format_spec.startswith('.') and format_spec[1:].replace('f', '').isdigit():
                # Apenas precisão: {num:.2f} -> "%.2f"
                format_string = f"%{format_spec}"
            else:
                # Formato desconhecido, usar padrão
                format_string = "%.6f"
        else:
            format_string = "%.6f"
        
        format_str = self._create_string_constant(format_string)
        
        # Chamar sprintf
        zero = ir.Constant(ir.IntType(32), 0)
        buffer_ptr = self.builder.gep(buffer, [zero, zero], inbounds=True)
        
        # Verificar se sprintf existe, se não, criar declaração
        if 'sprintf' not in self.module.globals:
            sprintf_type = ir.FunctionType(self.int_type, [self.char_type.as_pointer(), self.char_type.as_pointer()], var_arg=True)
            sprintf_func = ir.Function(self.module, sprintf_type, name='sprintf')
        else:
            sprintf_func = self.module.globals['sprintf']
        
        self.builder.call(sprintf_func, [buffer_ptr, format_str, value])
        return buffer_ptr
    
    def _bool_to_string(self, value: ir.Value) -> ir.Value:
        """Converte um bool para string"""
        # Usar um if para retornar "true" ou "false"
        true_str = self._create_string_constant("true")
        false_str = self._create_string_constant("false")
        
        # Comparar com true
        cond = self.builder.icmp_signed('==', value, ir.Constant(self.bool_type, True))
        
        # Usar select para escolher a string
        return self.builder.select(cond, true_str, false_str)
    
    def _concatenate_strings(self, str1: ir.Value, str2: ir.Value) -> ir.Value:
        """Concatena duas strings usando malloc + strcpy + strcat"""
        # Obter tamanhos das strings usando strlen
        if 'strlen' not in self.module.globals:
            strlen_type = ir.FunctionType(ir.IntType(64), [self.char_type.as_pointer()])
            strlen_func = ir.Function(self.module, strlen_type, name='strlen')
        else:
            strlen_func = self.module.globals['strlen']
        
        len1 = self.builder.call(strlen_func, [str1])
        len2 = self.builder.call(strlen_func, [str2])
        
        # Calcular tamanho total (len1 + len2 + 1 para null terminator)
        one = ir.Constant(ir.IntType(64), 1)
        total_len = self.builder.add(self.builder.add(len1, len2), one)
        
        # Alocar memória para a nova string
        if 'malloc' not in self.module.globals:
            malloc_type = ir.FunctionType(self.char_type.as_pointer(), [ir.IntType(64)])
            malloc_func = ir.Function(self.module, malloc_type, name='malloc')
        else:
            malloc_func = self.module.globals['malloc']
        
        result_ptr = self.builder.call(malloc_func, [total_len])
        
        # Copiar primeira string usando strcpy
        if 'strcpy' not in self.module.globals:
            strcpy_type = ir.FunctionType(self.char_type.as_pointer(), [self.char_type.as_pointer(), self.char_type.as_pointer()])
            strcpy_func = ir.Function(self.module, strcpy_type, name='strcpy')
        else:
            strcpy_func = self.module.globals['strcpy']
        
        self.builder.call(strcpy_func, [result_ptr, str1])
        
        # Concatenar segunda string usando strcat
        if 'strcat' not in self.module.globals:
            strcat_type = ir.FunctionType(self.char_type.as_pointer(), [self.char_type.as_pointer(), self.char_type.as_pointer()])
            strcat_func = ir.Function(self.module, strcat_type, name='strcat')
        else:
            strcat_func = self.module.globals['strcat']
        
        self.builder.call(strcat_func, [result_ptr, str2])
        
        return result_ptr

    def _generate_expression(self, node: ASTNode, expected_type: ir.Type = None) -> ir.Value:
        if node is None:
            raise ValueError("Tentativa de gerar código para nó None")
            
        if isinstance(node, NumberNode):
            return ir.Constant(self.int_type, node.value)
            
        elif isinstance(node, FloatNode):
            return ir.Constant(self.float_type, node.value)
                
        elif isinstance(node, StringNode):
            # Criar string global
            string_value = node.value + '\0'  # Adicionar null terminator
            # Converter para bytes para contar corretamente caracteres UTF-8
            string_bytes = string_value.encode('utf8')
            str_type = ir.ArrayType(self.char_type, len(string_bytes))
            str_name = f"str_{len(self.module.globals)}"
            
            str_global = ir.GlobalVariable(self.module, str_type, name=str_name)
            str_global.linkage = 'internal'
            str_global.global_constant = True
            str_global.initializer = ir.Constant(str_type, bytearray(string_bytes))
            
            # Retornar ponteiro para a string
            zero = ir.Constant(ir.IntType(32), 0)
            return self.builder.gep(str_global, [zero, zero], inbounds=True)
            
        elif isinstance(node, FStringNode):
            return self._generate_fstring(node)
            
        elif isinstance(node, BooleanNode):
            return ir.Constant(self.bool_type, node.value)
            
        elif isinstance(node, NullNode):
            if expected_type and isinstance(expected_type, ir.PointerType):
                return ir.Constant(expected_type, None)
            elif expected_type and isinstance(expected_type, ir.IntType):
                return ir.Constant(expected_type, 0)
            else:
                # Para null sem tipo específico, retornar ponteiro nulo genérico
                return ir.Constant(ir.IntType(8).as_pointer(), None)
            
        elif isinstance(node, ReferenceNode):
            # Gerar referência: ref expressao
            expr_value = self._generate_expression(node.expression)
            # Para referências, retornar o endereço da expressão
            if isinstance(expr_value.type, ir.PointerType):
                return expr_value
            else:
                # Se não é um ponteiro, retornar o valor como está
                return expr_value
            
        elif isinstance(node, StructAccessNode):
            # Acessar campo de struct (suporta acesso aninhado)
            # node.struct_name é o nome da variável (ex: "node")
            # node.field_name é o nome do campo (ex: "valor")
            
            # Procurar a variável primeiro localmente, depois globalmente
            if node.struct_name in self.local_vars:
                struct_ptr = self.local_vars[node.struct_name]
                # Verificar se é um parâmetro de função (ponteiro para ponteiro)
                if isinstance(struct_ptr, ir.Argument):
                    # É um parâmetro de função, precisa dereferenciar
                    struct_ptr = self.builder.load(struct_ptr)
                elif (node.struct_name in self.type_map and 
                      isinstance(self.type_map[node.struct_name], ReferenceType)):
                    # É uma referência, precisa dereferenciar
                    struct_ptr = self.builder.load(struct_ptr)
                # NOVO: Se for um alloca (variável local), fazer load
                import llvmlite.ir.instructions
                if isinstance(struct_ptr, llvmlite.ir.instructions.AllocaInstr):
                    struct_ptr = self.builder.load(struct_ptr)
            elif node.struct_name in self.global_vars:
                struct_ptr = self.global_vars[node.struct_name]
                # Se for uma GlobalVariable cujo pointee é ponteiro (ex.: Node**), carregar para obter Node*
                from llvmlite import ir as _ir
                if isinstance(struct_ptr, _ir.GlobalVariable):
                    if isinstance(struct_ptr.type, _ir.PointerType) and isinstance(struct_ptr.type.pointee, _ir.PointerType):
                        struct_ptr = self.builder.load(struct_ptr)
            else:
                # Procurar nas variáveis globais do módulo
                struct_ptr = self.module.globals.get(node.struct_name)
                if struct_ptr is None:
                    raise NameError(f"Variável '{node.struct_name}' não encontrada")
            if (isinstance(struct_ptr.type, ir.PointerType)
                    and isinstance(struct_ptr.type.pointee, ir.PointerType)):
                struct_ptr = self.builder.load(struct_ptr)
            
            # Determinar o tipo de struct baseado na variável
            struct_type_name = None
            if node.struct_name in self.type_map:
                var_type = self.type_map[node.struct_name]
                if isinstance(var_type, StructType):
                    struct_type_name = var_type.name
                elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                    struct_type_name = var_type.target_type.name
            
            if not struct_type_name or struct_type_name not in self.struct_fields:
                # Se não encontrou o tipo, tentar inferir do nome da variável
                # Isso é um fallback para casos onde o tipo não foi mapeado corretamente
                raise NameError(f"Não foi possível determinar o tipo da variável '{node.struct_name}'")
            
            # Fazer cast do ponteiro para void para o tipo correto do struct
            if struct_type_name in self.struct_types:
                struct_type = self.struct_types[struct_type_name]
                struct_ptr = self.builder.bitcast(struct_ptr, struct_type.as_pointer())
            
            # Verificar se é acesso aninhado (ex: pessoa.endereco.rua)
            if '.' in node.field_name:
                # Dividir o caminho: "endereco.rua" -> ["endereco", "rua"]
                field_path = node.field_name.split('.')
                
                # Navegar pelo caminho
                current_ptr = struct_ptr
                current_struct_type = struct_type_name
                
                for i, field_name in enumerate(field_path):
                    if current_struct_type not in self.struct_fields or field_name not in self.struct_fields[current_struct_type]:
                        self._semantic_error(f"Campo '{field_name}' não encontrado em struct '{current_struct_type}'", node)
                    
                    # Obter o índice do campo
                    field_index = self.struct_fields[current_struct_type][field_name]
                    
                    # Verificar se current_ptr é um ponteiro válido
                    if not isinstance(current_ptr.type, ir.PointerType):
                        raise TypeError(f"Tentativa de acessar campo '{field_name}' em valor que não é um ponteiro: {current_ptr.type}")
                    
                    # Acessar o campo
                    field_ptr = self.builder.gep(current_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                    
                    # Se não é o último campo, continuar navegando
                    if i < len(field_path) - 1:
                        # Determinar o tipo do campo para continuar navegando
                        if hasattr(self, 'struct_field_types') and current_struct_type in self.struct_field_types:
                            field_type = self.struct_field_types[current_struct_type][field_name]
                            if isinstance(field_type, StructType):
                                # Para structs embutidos, field_ptr já aponta para o local correto
                                # Mas precisamos fazer cast para o tipo correto
                                if field_type.name in self.struct_types:
                                    target_struct_type = self.struct_types[field_type.name]
                                    current_ptr = self.builder.bitcast(field_ptr, target_struct_type.as_pointer())
                                else:
                                    current_ptr = field_ptr
                                current_struct_type = field_type.name
                            elif isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                                # É uma referência para um struct
                                current_struct_type = field_type.target_type.name
                                # Para referências, precisamos carregar o valor e fazer cast para o tipo correto
                                if field_type.target_type.name in self.struct_types:
                                    target_struct_type = self.struct_types[field_type.target_type.name]
                                    loaded_value = self.builder.load(field_ptr)
                                    current_ptr = self.builder.bitcast(loaded_value, target_struct_type.as_pointer())
                                else:
                                    raise NameError(f"Tipo de struct '{field_type.target_type.name}' não encontrado")
                            else:
                                raise NameError(f"Campo '{field_name}' não é um struct ou referência para struct")
                        else:
                            # Fallback: tentar inferir o tipo do campo
                            # Se o campo é uma referência para um struct, usar o nome do struct
                            if (hasattr(self, 'struct_field_types') and 
                                current_struct_type in self.struct_field_types and 
                                field_name in self.struct_field_types[current_struct_type]):
                                field_type = self.struct_field_types[current_struct_type][field_name]
                                if isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                                    current_struct_type = field_type.target_type.name
                                elif isinstance(field_type, StructType):
                                    current_struct_type = field_type.name
                                else:
                                    # Se não conseguir determinar, usar o nome do campo como último recurso
                                    current_struct_type = field_name
                            else:
                                # Se não conseguir determinar, usar o nome do campo como último recurso
                                current_struct_type = field_name
                    else:
                        # Último campo, retornar o valor
                        return self.builder.load(field_ptr)
            else:
                # Acesso simples a campo
                if node.field_name not in self.struct_fields[struct_type_name]:
                    self._semantic_error(f"Campo '{node.field_name}' não encontrado em struct '{struct_type_name}'", node)
                
                # Obter o índice do campo
                field_index = self.struct_fields[struct_type_name][node.field_name]
                
                # Verificar se struct_ptr é um ponteiro válido
                if not isinstance(struct_ptr.type, ir.PointerType):
                    raise TypeError(f"Tentativa de acessar campo '{node.field_name}' em valor que não é um ponteiro: {struct_ptr.type}")
                
                # Acessar o campo usando getelementptr diretamente no ponteiro
                field_ptr = self.builder.gep(struct_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                
                # Verificar se o campo é uma referência (ref TreeNode)
                if (hasattr(self, 'struct_field_types') and 
                    struct_type_name in self.struct_field_types and 
                    node.field_name in self.struct_field_types[struct_type_name]):
                    field_type = self.struct_field_types[struct_type_name][node.field_name]
                    if isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                        # É uma referência, carregar o valor do campo (que é um ponteiro)
                        field_value = self.builder.load(field_ptr)
                        # Fazer cast para o tipo correto do struct
                        if field_type.target_type.name in self.struct_types:
                            struct_type = self.struct_types[field_type.target_type.name]
                            return self.builder.bitcast(field_value, struct_type.as_pointer())
                        else:
                            return field_value
                
                # Verificar se o campo é um array
                if (hasattr(self, 'struct_field_types') and 
                    struct_type_name in self.struct_field_types and 
                    node.field_name in self.struct_field_types[struct_type_name]):
                    field_type = self.struct_field_types[struct_type_name][node.field_name]
                    if isinstance(field_type, ArrayType):
                        # Para arrays, retornar o ponteiro do campo (não carregar o valor)
                        return field_ptr
                
                # Para outros tipos, carregar o valor do campo
                field_value = self.builder.load(field_ptr)
                
                # Verificar se o tipo do campo foi definido e fazer cast se necessário
                if (hasattr(self, 'struct_field_types') and 
                    struct_type_name in self.struct_field_types and 
                    node.field_name in self.struct_field_types[struct_type_name]):
                    field_type = self.struct_field_types[struct_type_name][node.field_name]
                    expected_llvm_type = self._convert_type(field_type)
                    
                    # Se o tipo carregado não corresponde ao tipo esperado, fazer cast
                    if field_value.type != expected_llvm_type:
                        if hasattr(field_value.type, 'width') and hasattr(expected_llvm_type, 'width'):
                            if field_value.type.width < expected_llvm_type.width:
                                # Extensão de sinal para tipos menores
                                if field_value.type.width == 8 and expected_llvm_type.width == 64:
                                    field_value = self.builder.sext(field_value, expected_llvm_type)
                                else:
                                    field_value = self.builder.bitcast(field_value, expected_llvm_type)
                            elif field_value.type.width > expected_llvm_type.width:
                                # Truncamento para tipos maiores
                                field_value = self.builder.trunc(field_value, expected_llvm_type)
                            else:
                                # Mesmo tamanho mas tipos diferentes, fazer bitcast
                                field_value = self.builder.bitcast(field_value, expected_llvm_type)
                        else:
                            # Para tipos sem width (como ponteiros), usar bitcast
                            field_value = self.builder.bitcast(field_value, expected_llvm_type)
                
                return field_value
        elif isinstance(node, ArrayNode):
            # Alocar memória para o array
            num_elements = len(node.elements)

            # Preferir tipo esperado (quando inicializando arrays globais ou contexto conhecido)
            inferred_from_expected = False
            element_ptr_type = None
            if expected_type is not None and isinstance(expected_type, ir.PointerType):
                element_ptr_type = expected_type
                inferred_from_expected = True

            if inferred_from_expected:
                # Determinar caminho com base no tipo apontado
                pointee = element_ptr_type.pointee
                # Tamanho por elemento para ponteiros e tipos escalares
                if isinstance(pointee, ir.IntType) and pointee.width == 64:
                    elem_size = 8
                    array_size = ir.Constant(self.int_type, num_elements * elem_size)
                    array_ptr = self.builder.call(self.malloc, [array_size])
                    self._track_allocation(array_ptr)
                    typed_ptr = self.builder.bitcast(array_ptr, element_ptr_type)
                elif isinstance(pointee, ir.DoubleType):
                    elem_size = 8
                    array_size = ir.Constant(self.int_type, num_elements * elem_size)
                    array_ptr = self.builder.call(self.malloc, [array_size])
                    self._track_allocation(array_ptr)
                    typed_ptr = self.builder.bitcast(array_ptr, element_ptr_type)
                elif isinstance(pointee, ir.IntType) and pointee.width == 1:
                    elem_size = 1
                    array_size = ir.Constant(self.int_type, num_elements * elem_size)
                    array_ptr = self.builder.call(self.malloc, [array_size])
                    self._track_allocation(array_ptr)
                    typed_ptr = self.builder.bitcast(array_ptr, element_ptr_type)
                elif isinstance(pointee, ir.PointerType):
                    # Ponteiro para ponteiro (ex.: struct** ou i8** para strings)
                    elem_size = 8
                    array_size = ir.Constant(self.int_type, num_elements * elem_size)
                    array_ptr = self.builder.call(self.malloc, [array_size])
                    self._track_allocation(array_ptr)
                    typed_ptr = self.builder.bitcast(array_ptr, element_ptr_type)
                else:
                    # Fallback simples
                    elem_size = 8
                    array_size = ir.Constant(self.int_type, num_elements * elem_size)
                    array_ptr = self.builder.call(self.malloc, [array_size])
                    self._track_allocation(array_ptr)
                    typed_ptr = array_ptr
            elif isinstance(node.element_type, IntType):
                elem_size = 8
                array_size = ir.Constant(self.int_type, num_elements * elem_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                typed_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
            elif isinstance(node.element_type, FloatType):
                elem_size = 8
                array_size = ir.Constant(self.int_type, num_elements * elem_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                typed_ptr = self.builder.bitcast(array_ptr, self.float_type.as_pointer())
            elif isinstance(node.element_type, StringType) or isinstance(node.element_type, StrType):
                elem_size = 8  # ponteiro de 64 bits
                array_size = ir.Constant(self.int_type, num_elements * elem_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                # Ponteiro para ponteiro de char (i8**)
                typed_ptr = self.builder.bitcast(array_ptr, self.char_type.as_pointer().as_pointer())
            elif isinstance(node.element_type, BoolType):
                elem_size = 1  # 1 byte para booleanos
                array_size = ir.Constant(self.int_type, num_elements * elem_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                typed_ptr = self.builder.bitcast(array_ptr, self.bool_type.as_pointer())
            elif isinstance(node.element_type, StructType):
                # Array de structs por valor (não ponteiros)
                struct_name = node.element_type.name
                if struct_name not in self.struct_types:
                    raise NameError(f"Struct '{struct_name}' não definido")
                struct_lltype = self.struct_types[struct_name]
                
                # Calcular tamanho de cada struct
                struct_size = struct_lltype.get_abi_size(self.target_data)
                total_size = struct_size * num_elements
                array_size = ir.Constant(self.int_type, total_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                
                # Criar tipo array de structs por valor
                array_type = ir.ArrayType(struct_lltype, num_elements)
                typed_ptr = self.builder.bitcast(array_ptr, array_type.as_pointer())
            else:
                elem_size = 1
                array_size = ir.Constant(self.int_type, num_elements * elem_size)
                array_ptr = self.builder.call(self.malloc, [array_size])
                self._track_allocation(array_ptr)
                typed_ptr = array_ptr

            # Inicializar elementos
            for i, elem in enumerate(node.elements):
                value = self._generate_expression(elem)
                if inferred_from_expected:
                    pointee = element_ptr_type.pointee
                    elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                    # Inteiros
                    if isinstance(pointee, ir.IntType) and pointee.width == 64:
                        if value.type != self.int_type:
                            # Tentar converter numérico
                            if isinstance(value.type, ir.DoubleType):
                                value = self.builder.fptosi(value, self.int_type)
                            elif isinstance(value.type, ir.IntType):
                                value = self.builder.sext(value, self.int_type) if value.type.width < 64 else value
                            else:
                                value = self.builder.bitcast(value, self.int_type)
                        self.builder.store(value, elem_ptr)
                    # Doubles
                    elif isinstance(pointee, ir.DoubleType):
                        if value.type != self.float_type:
                            if isinstance(value.type, ir.IntType):
                                value = self.builder.sitofp(value, self.float_type)
                            else:
                                value = self.builder.bitcast(value, self.float_type)
                        self.builder.store(value, elem_ptr)
                    # Bool
                    elif isinstance(pointee, ir.IntType) and pointee.width == 1:
                        if value.type != self.bool_type:
                            value = self.builder.icmp_ne(value, ir.Constant(value.type, 0)) if hasattr(value.type, 'width') else value
                        self.builder.store(value, elem_ptr)
                    # Ponteiros (string i8* ou struct*)
                    elif isinstance(pointee, ir.PointerType):
                        # Ajustar valor para o tipo esperado
                        if isinstance(value.type, ir.PointerType) and value.type != pointee:
                            value = self.builder.bitcast(value, pointee)
                        self.builder.store(value, elem_ptr)
                    else:
                        # Fallback
                        self.builder.store(value, elem_ptr)
                elif isinstance(node.element_type, StringType) or isinstance(node.element_type, StrType):
                    value = self.builder.bitcast(value, self.char_type.as_pointer())
                    elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                    # Forçar o ponteiro do elemento para i8** explicitamente
                    elem_ptr = self.builder.bitcast(elem_ptr, self.char_type.as_pointer().as_pointer())
                    self.builder.store(value, elem_ptr)
                elif isinstance(node.element_type, BoolType):
                    elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                    # Garantir que o valor é do tipo correto
                    if value.type != self.bool_type:
                        value = self.builder.icmp_ne(value, ir.Constant(value.type, 0))
                    self.builder.store(value, elem_ptr)
                elif isinstance(node.element_type, StructType):
                    # Armazenar struct por valor no array
                    elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                    struct_lltype = self.struct_types[node.element_type.name]
                    
                    # Verificar se elem_ptr aponta para o tipo correto
                    expected_ptr_type = struct_lltype.as_pointer()
                    if elem_ptr.type != expected_ptr_type:
                        elem_ptr = self.builder.bitcast(elem_ptr, expected_ptr_type)
                    
                    # Se o valor é um ponteiro para struct, carregar o valor
                    if hasattr(value, 'type') and isinstance(value.type, ir.PointerType):
                        if isinstance(value.type.pointee, (ir.LiteralStructType, ir.IdentifiedStructType)):
                            # Garantir que o ponteiro é do tipo correto
                            if value.type != expected_ptr_type:
                                value = self.builder.bitcast(value, expected_ptr_type)
                            # Carregar o struct por valor
                            struct_value = self.builder.load(value)
                            self.builder.store(struct_value, elem_ptr)
                        else:
                            # Bitcast se necessário
                            if value.type != expected_ptr_type:
                                value = self.builder.bitcast(value, expected_ptr_type)
                            struct_value = self.builder.load(value)
                            self.builder.store(struct_value, elem_ptr)
                    else:
                        # Valor já é um struct por valor
                        self.builder.store(value, elem_ptr)
                else:
                    elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                    self.builder.store(value, elem_ptr)

            # Se o array foi alocado localmente (com alloca), retornar ponteiro para o primeiro elemento
            if hasattr(node, 'is_local') and node.is_local:
                zero = ir.Constant(self.int_type, 0)
                return self.builder.gep(typed_ptr, [zero, zero], inbounds=True)
            return typed_ptr
            
        elif isinstance(node, ZerosNode):
            # Syntax sugar para criar arrays preenchidos com zeros
            size = node.size
            elem_size = 8  # 8 bytes para int ou float
            array_size = ir.Constant(self.int_type, size * elem_size)
            array_ptr = self.builder.call(self.malloc, [array_size])
            self._track_allocation(array_ptr)
            
            # Cast e inicializar com zeros
            if isinstance(node.element_type, IntType):
                typed_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                zero_val = ir.Constant(self.int_type, 0)
            else:
                typed_ptr = self.builder.bitcast(array_ptr, self.float_type.as_pointer())
                zero_val = ir.Constant(self.float_type, 0.0)
            
            # Loop para inicializar com zeros
            for i in range(size):
                elem_ptr = self.builder.gep(typed_ptr, [ir.Constant(self.int_type, i)], inbounds=True)
                self.builder.store(zero_val, elem_ptr)
            
            return typed_ptr
            
        elif isinstance(node, CastNode):
            # Implementar conversões de tipo
            expr_value = self._generate_expression(node.expression)
            
            if isinstance(node.target_type, IntType):
                # Converter para int
                if isinstance(expr_value.type, ir.DoubleType):
                    # float -> int
                    return self.builder.fptosi(expr_value, self.int_type, name="ftoi")
                elif expr_value.type == self.string_type or (isinstance(expr_value.type, ir.PointerType) and expr_value.type.pointee == self.char_type):
                    # string -> int (usando atoi simulado)
                    # Por simplicidade, vamos retornar 0
                    return ir.Constant(self.int_type, 0)
                else:
                    return expr_value
                    
            elif isinstance(node.target_type, FloatType):
                # Converter para float
                if isinstance(expr_value.type, ir.IntType):
                    # int -> float
                    return self.builder.sitofp(expr_value, self.float_type, name="itof")
                else:
                    return expr_value
                    
            elif isinstance(node.target_type, StringType):
                # Converter para string
                buffer_size = ir.Constant(self.int_type, 256)
                buffer = self.builder.call(self.malloc, [buffer_size])
                self._track_allocation(buffer)
                
                if isinstance(expr_value.type, ir.IntType):
                    # int -> string
                    fmt_str = "%lld\0"
                    fmt_bytes = fmt_str.encode('utf8')
                    fmt_type = ir.ArrayType(self.char_type, len(fmt_bytes))
                    fmt_name = f"fmt_itoa_{len(self.module.globals)}"
                    fmt_global = ir.GlobalVariable(self.module, fmt_type, name=fmt_name)
                    fmt_global.linkage = 'internal'
                    fmt_global.global_constant = True
                    fmt_global.initializer = ir.Constant(fmt_type, bytearray(fmt_bytes))
                    zero = ir.Constant(ir.IntType(32), 0)
                    fmt_ptr = self.builder.gep(fmt_global, [zero, zero], inbounds=True)
                    
                    self.builder.call(self.sprintf, [buffer, fmt_ptr, expr_value])
                    
                elif isinstance(expr_value.type, ir.DoubleType):
                    # float -> string
                    fmt_str = "%f\0"
                    fmt_bytes = fmt_str.encode('utf8')
                    fmt_type = ir.ArrayType(self.char_type, len(fmt_bytes))
                    fmt_name = f"fmt_ftoa_{len(self.module.globals)}"
                    fmt_global = ir.GlobalVariable(self.module, fmt_type, name=fmt_name)
                    fmt_global.linkage = 'internal'
                    fmt_global.global_constant = True
                    fmt_global.initializer = ir.Constant(fmt_type, bytearray(fmt_bytes))
                    zero = ir.Constant(ir.IntType(32), 0)
                    fmt_ptr = self.builder.gep(fmt_global, [zero, zero], inbounds=True)
                    
                    self.builder.call(self.sprintf, [buffer, fmt_ptr, expr_value])
                else:
                    # Já é string
                    return expr_value
                
                return buffer
                
            elif isinstance(node.target_type, BoolType):
                # Converter para bool
                if isinstance(expr_value.type, ir.IntType):
                    # int -> bool (não-zero é true, zero é false)
                    return self.builder.icmp_signed('!=', expr_value, ir.Constant(expr_value.type, 0))
                elif isinstance(expr_value.type, ir.DoubleType):
                    # float -> bool (não-zero é true, zero é false)
                    return self.builder.fcmp_ordered('!=', expr_value, ir.Constant(expr_value.type, 0.0))
                else:
                    # Já é bool ou outro tipo
                    return expr_value
                
        elif isinstance(node, ConcatNode):
            # Concatenação de strings
            left_str = self._generate_expression(node.left)
            right_str = self._generate_expression(node.right)
            
            # Calcular tamanho necessário
            len1 = self.builder.call(self.strlen, [left_str])
            len2 = self.builder.call(self.strlen, [right_str])
            total_len = self.builder.add(len1, len2)
            total_len = self.builder.add(total_len, ir.Constant(self.int_type, 1))  # +1 para null terminator
            
            # Alocar memória para resultado
            result = self.builder.call(self.malloc, [total_len])
            self._track_allocation(result)
            
            # Copiar primeira string
            self.builder.call(self.strcpy, [result, left_str])
            
            # Concatenar segunda string
            self.builder.call(self.strcat, [result, right_str])
            
            return result
            
        elif isinstance(node, ArrayAccessNode):
            # Verificar se é um acesso a campo de struct (ex: arr.elementos[i])
            if '.' in node.array_name:
                # Dividir o nome: "arr.elementos" -> ["arr", "elementos"]
                parts = node.array_name.split('.')
                struct_name = parts[0]
                field_name = parts[1]
                
                # Procurar a variável struct primeiro localmente, depois globalmente
                if struct_name in self.local_vars:
                    struct_ptr = self.local_vars[struct_name]
                    # Verificar se é um parâmetro de função (ponteiro para ponteiro)
                    if isinstance(struct_ptr, ir.Argument):
                        # É um parâmetro de função, precisa dereferenciar
                        struct_ptr = self.builder.load(struct_ptr)
                    elif (struct_name in self.type_map and 
                          isinstance(self.type_map[struct_name], ReferenceType)):
                        # É uma referência, precisa dereferenciar
                        struct_ptr = self.builder.load(struct_ptr)
                    # Se for um alloca (variável local), fazer load
                    import llvmlite.ir.instructions
                    if isinstance(struct_ptr, llvmlite.ir.instructions.AllocaInstr):
                        struct_ptr = self.builder.load(struct_ptr)
                elif struct_name in self.global_vars:
                    struct_ptr = self.global_vars[struct_name]
                    # Se for uma GlobalVariable cujo pointee é ponteiro (ex.: JsonObject**), carregar para obter JsonObject*
                    from llvmlite import ir as _ir
                    if isinstance(struct_ptr, _ir.GlobalVariable):
                        if isinstance(struct_ptr.type, _ir.PointerType) and isinstance(struct_ptr.type.pointee, _ir.PointerType):
                            struct_ptr = self.builder.load(struct_ptr)
                else:
                    raise NameError(f"Variável '{struct_name}' não encontrada")
                
                # Determinar o tipo de struct baseado na variável
                struct_type_name = None
                if struct_name in self.type_map:
                    var_type = self.type_map[struct_name]
                    if isinstance(var_type, StructType):
                        struct_type_name = var_type.name
                    elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                        struct_type_name = var_type.target_type.name
                
                if not struct_type_name or struct_type_name not in self.struct_fields:
                    raise NameError(f"Não foi possível determinar o tipo da variável '{struct_name}'")
                
                # Verificar se o campo existe
                if field_name not in self.struct_fields[struct_type_name]:
                    raise NameError(f"Campo '{field_name}' não encontrado em struct '{struct_type_name}'")
                
                # Fazer cast do ponteiro para o tipo correto do struct
                if struct_type_name in self.struct_types:
                    struct_type = self.struct_types[struct_type_name]
                    struct_ptr = self.builder.bitcast(struct_ptr, struct_type.as_pointer())
                
                # Obter o índice do campo
                field_index = self.struct_fields[struct_type_name][field_name]
                
                # Acessar o campo do struct
                field_ptr = self.builder.gep(struct_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                
                # O campo é um array, obter ponteiro para o primeiro elemento
                if isinstance(field_ptr.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.ArrayType):
                    zero = ir.Constant(ir.IntType(32), 0)
                    array_ptr = self.builder.gep(field_ptr, [zero, zero], inbounds=True)
                elif isinstance(field_ptr.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.PointerType) and field_ptr.type.pointee.pointee == self.char_type:
                    # É um campo string (i8**), carregar o ponteiro da string primeiro
                    array_ptr = self.builder.load(field_ptr)
                else:
                    array_ptr = field_ptr
                
                # Continuar com a lógica normal de acesso a array
                index = self._generate_expression(node.index)
                
                # Se for string (i8*), acessar como caractere (cast de segurança)
                if (isinstance(array_ptr.type, ir.PointerType) and array_ptr.type.pointee == self.char_type) or (
                    hasattr(node, 'element_type') and isinstance(node.element_type, StringType)):
                    if not (isinstance(array_ptr.type, ir.PointerType) and array_ptr.type.pointee == self.char_type):
                        array_ptr = self.builder.bitcast(array_ptr, self.char_type.as_pointer())
                    # Converter índice para i32 para compatibilidade com ponteiros i8*
                    if index.type != ir.IntType(32):
                        index = self.builder.sext(index, ir.IntType(32)) if index.type.width < 32 else self.builder.trunc(index, ir.IntType(32))
                    elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
                    char_value = self.builder.load(elem_ptr)
                    # Converter automaticamente char para string em Noxy
                    return self.builder.call(self.char_to_str, [char_value])
                
                # Caso contrário, array normal
                # Se for um array local (alocado com alloca), usar GEP [0, index]
                if isinstance(array_ptr.type, ir.ArrayType):
                    zero = ir.Constant(ir.IntType(32), 0)
                    elem_ptr = self.builder.gep(array_ptr, [zero, index], inbounds=True)
                    return self.builder.load(elem_ptr)
                else:
                    # Verificar se array_ptr é um ponteiro válido
                    if not isinstance(array_ptr.type, ir.PointerType):
                        # Se não é ponteiro, fazer cast para ponteiro
                        array_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                    elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
                    return self.builder.load(elem_ptr)
            
            # Caso normal: array simples
            # Procurar variável primeiro localmente, depois globalmente
            if node.array_name in self.local_vars:
                var = self.local_vars[node.array_name]
            elif node.array_name in self.global_vars:
                var = self.global_vars[node.array_name]
            else:
                raise NameError(f"Array '{node.array_name}' não definido")
            index = self._generate_expression(node.index)
            # Se a variável já é um ponteiro (parâmetro de função), usar diretamente
            if isinstance(var, ir.Argument) and isinstance(var.type, ir.PointerType):
                array_ptr = var
            elif isinstance(var, ir.GlobalVariable) and isinstance(var.type.pointee, ir.ArrayType):
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
            else:
                # Para arrays locais, não fazer load, usar diretamente o ponteiro
                if isinstance(var.type, ir.PointerType) and isinstance(var.type.pointee, ir.ArrayType):
                    zero = ir.Constant(ir.IntType(32), 0)
                    array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
                else:
                    # Verificar se é uma referência
                    if (node.array_name in self.type_map and 
                        isinstance(self.type_map[node.array_name], ReferenceType)):
                        # É uma referência, fazer load para obter o ponteiro real
                        array_ptr = self.builder.load(var)
                        # Se o resultado é um ponteiro para array, obter ponteiro para primeiro elemento
                        if isinstance(array_ptr.type, ir.PointerType) and isinstance(array_ptr.type.pointee, ir.ArrayType):
                            zero = ir.Constant(ir.IntType(32), 0)
                            array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)
                    else:
                        array_ptr = self.builder.load(var)
                        if isinstance(array_ptr.type, ir.ArrayType):
                            zero = ir.Constant(ir.IntType(32), 0)
                            array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)
            # Se for string (i8*), acessar como caractere e converter para string
            if (isinstance(array_ptr.type, ir.PointerType) and array_ptr.type.pointee == self.char_type) or (
                hasattr(node, 'element_type') and isinstance(node.element_type, StringType)) or (
                node.array_name in self.type_map and isinstance(self.type_map[node.array_name], StringType)):
                if not (isinstance(array_ptr.type, ir.PointerType) and array_ptr.type.pointee == self.char_type):
                    array_ptr = self.builder.bitcast(array_ptr, self.char_type.as_pointer())
                # Converter índice para i32 para compatibilidade com ponteiros i8*
                if index.type != ir.IntType(32):
                    index = self.builder.sext(index, ir.IntType(32)) if index.type.width < 32 else self.builder.trunc(index, ir.IntType(32))
                elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
                char_value = self.builder.load(elem_ptr)
                # Converter caractere para string usando char_to_str
                char_str = self.builder.call(self.char_to_str, [char_value])
                return char_str
            # Caso contrário, array normal
            # Se for um array local (alocado com alloca), usar GEP [0, index]
            if isinstance(array_ptr.type, ir.ArrayType):
                zero = ir.Constant(ir.IntType(32), 0)
                elem_ptr = self.builder.gep(array_ptr, [zero, index], inbounds=True)
                return self.builder.load(elem_ptr)
            else:
                # Verificar se array_ptr é um ponteiro válido
                if not isinstance(array_ptr.type, ir.PointerType):
                    # Se não é ponteiro, fazer cast para ponteiro
                    array_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
                return self.builder.load(elem_ptr)
            
        elif isinstance(node, StructAccessFromArrayNode):
            # Gerar o ponteiro/valor do elemento do array
            base = node.base_access
            # Reaproveitar caminho de acesso a array (similar ao caso ArrayAccessNode simples)
            if base.array_name in self.local_vars:
                var = self.local_vars[base.array_name]
            elif base.array_name in self.global_vars:
                var = self.global_vars[base.array_name]
            else:
                raise NameError(f"Array '{base.array_name}' não definido")
            index = self._generate_expression(base.index)
            if isinstance(var, ir.Argument) and isinstance(var.type, ir.PointerType):
                array_ptr = var
            elif isinstance(var, ir.GlobalVariable) and isinstance(var.type.pointee, ir.ArrayType):
                zero = ir.Constant(ir.IntType(32), 0)
                array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
            else:
                if isinstance(var.type, ir.PointerType) and isinstance(var.type.pointee, ir.ArrayType):
                    zero = ir.Constant(ir.IntType(32), 0)
                    array_ptr = self.builder.gep(var, [zero, zero], inbounds=True)
                else:
                    # Verificar se é uma referência para array
                    if (base.array_name in self.type_map and 
                        isinstance(self.type_map[base.array_name], ReferenceType)):
                        array_ptr = self.builder.load(var)
                        if isinstance(array_ptr.type, ir.PointerType) and isinstance(array_ptr.type.pointee, ir.ArrayType):
                            zero = ir.Constant(ir.IntType(32), 0)
                            array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)
                    else:
                        array_ptr = self.builder.load(var)
                        if isinstance(array_ptr.type, ir.ArrayType):
                            zero = ir.Constant(ir.IntType(32), 0)
                            array_ptr = self.builder.gep(array_ptr, [zero, zero], inbounds=True)

            # Ponteiro para o elemento
            elem_ptr = self.builder.gep(array_ptr, [index], inbounds=True)
            # Carregar ponteiro para struct armazenado no array
            struct_ptr = self.builder.load(elem_ptr)

            # Descobrir tipo do struct via tipo do array em type_map
            struct_name = None
            if base.array_name in self.type_map and isinstance(self.type_map[base.array_name], ArrayType):
                elem_type = self.type_map[base.array_name].element_type
                if isinstance(elem_type, StructType):
                    struct_name = elem_type.name
                elif isinstance(elem_type, ReferenceType) and isinstance(elem_type.target_type, StructType):
                    struct_name = elem_type.target_type.name
            if not struct_name or struct_name not in self.struct_fields:
                # Fallback: tratar como ponteiro para void, apenas retornar ponteiro
                return struct_ptr

            # Cast para o tipo correto do struct
            if struct_name in self.struct_types:
                struct_lltype = self.struct_types[struct_name]
                struct_ptr = self.builder.bitcast(struct_ptr, struct_lltype.as_pointer())

            # Navegar nos campos conforme field_path
            current_ptr = struct_ptr
            current_struct_type = struct_name
            for i, field_name in enumerate(node.field_path.split('.')):
                if current_struct_type not in self.struct_fields or field_name not in self.struct_fields[current_struct_type]:
                    raise NameError(f"Campo '{field_name}' não encontrado em struct '{current_struct_type}'")
                field_index = self.struct_fields[current_struct_type][field_name]
                # Acessar o campo
                field_ptr = self.builder.gep(current_ptr, [ir.Constant(ir.IntType(32), 0), ir.Constant(ir.IntType(32), field_index)])
                if i < len(node.field_path.split('.')) - 1:
                    # Avançar para struct interno
                    next_type = None
                    if hasattr(self, 'struct_field_types') and current_struct_type in self.struct_field_types:
                        next_type = self.struct_field_types[current_struct_type][field_name]
                    if isinstance(next_type, StructType):
                        current_struct_type = next_type.name
                        current_ptr = field_ptr
                    elif isinstance(next_type, ReferenceType) and isinstance(next_type.target_type, StructType):
                        current_struct_type = next_type.target_type.name
                        loaded = self.builder.load(field_ptr)
                        if current_struct_type in self.struct_types:
                            current_ptr = self.builder.bitcast(loaded, self.struct_types[current_struct_type].as_pointer())
                        else:
                            current_ptr = loaded
                    else:
                        raise NameError(f"Campo '{field_name}' não é um struct")
                else:
                    # Último campo: retornar valor carregado
                    return self.builder.load(field_ptr)

        elif isinstance(node, IdentifierNode):
            # Verificar se é um nome com ponto (acesso multi-nível como utils.math.pi)
            if '.' in node.name and node.name in self.imported_symbols:
                # É uma variável importada com nome completo
                symbol_info = self.imported_symbols[node.name]
                if symbol_info['type'] == 'variable':
                    # Extrair o nome simples da variável
                    simple_name = node.name.split('.')[-1]
                    # Gerar declaração da variável importada se ainda não existir
                    if simple_name not in self.global_vars:
                        var_node = symbol_info['node']
                        self._declare_global_variable(var_node)
                    var = self.global_vars[simple_name]
                    # Se é um array global, retornar ponteiro para o início
                    if isinstance(var.type.pointee, ir.ArrayType):
                        zero = ir.Constant(ir.IntType(32), 0)
                        return self.builder.gep(var, [zero, zero], inbounds=True)
                    # Senão, carregar o valor
                    return self.builder.load(var, name=simple_name)
                else:
                    self._semantic_error(f"'{node.name}' foi importado mas não é uma variável", node)
            # Procurar variável primeiro localmente, depois globalmente
            elif node.name in self.local_vars:
                var = self.local_vars[node.name]
                # Se é um parâmetro de função que é array, retornar diretamente
                if isinstance(var, ir.Argument) and isinstance(var.type, ir.PointerType):
                    return var
                # Se é um array estático local, retornar o ponteiro (não fazer load)
                if isinstance(var.type, ir.PointerType) and isinstance(var.type.pointee, ir.ArrayType):
                    return var
                # Se é um ponteiro (ref), retornar ponteiro diretamente
                if isinstance(var.type, ir.PointerType) and var.type.pointee == ir.IntType(8):
                    return self.builder.load(var, name=node.name)
                # Senão, carregar o valor
                return self.builder.load(var, name=node.name)
            elif node.name in self.global_vars:
                var = self.global_vars[node.name]
                # Se é um array global, retornar ponteiro para o início
                if isinstance(var.type.pointee, ir.ArrayType):
                    zero = ir.Constant(ir.IntType(32), 0)
                    return self.builder.gep(var, [zero, zero], inbounds=True)
                # Se é um ponteiro (ref), retornar ponteiro diretamente
                if isinstance(var.type, ir.PointerType) and var.type.pointee == ir.IntType(8):
                    return self.builder.load(var, name=node.name)
                # Senão, carregar o valor
                return self.builder.load(var, name=node.name)
            elif node.name in self.imported_symbols:
                # Variável importada
                symbol_info = self.imported_symbols[node.name]
                if symbol_info['type'] == 'variable':
                    # Gerar declaração da variável importada se ainda não existir
                    if node.name not in self.global_vars:
                        var_node = symbol_info['node']
                        self._declare_global_variable(var_node)
                    var = self.global_vars[node.name]
                    # Se é um array global, retornar ponteiro para o início
                    if isinstance(var.type.pointee, ir.ArrayType):
                        zero = ir.Constant(ir.IntType(32), 0)
                        return self.builder.gep(var, [zero, zero], inbounds=True)
                    # Senão, carregar o valor
                    return self.builder.load(var, name=node.name)
                else:
                    self._semantic_error(f"'{node.name}' foi importado mas não é uma variável", node)
            # Verificar se é um namespace (tem símbolos importados com este prefixo)
            elif any(symbol_name.startswith(f"{node.name}.") for symbol_name in self.imported_symbols.keys()):
                # É um namespace, mas não deveria ser resolvido isoladamente
                # Isso indica um erro no parser - namespace deveria fazer parte de uma expressão maior
                self._semantic_error(f"Namespace '{node.name}' não pode ser usado como variável. Use '{node.name}.simbolo' para acessar símbolos do namespace.", node)
            # Se contém pontos, pode ser acesso a struct que não foi encontrado como import
            elif '.' in node.name:
                # Tentar como struct access: converter IdentifierNode de volta para StructAccessNode
                parts = node.name.split('.')
                if len(parts) == 2:
                    # Simples: struct.field
                    return self._generate_expression(StructAccessNode(parts[0], parts[1]))
                elif len(parts) == 3:
                    # Complexo: struct.field.subfield - criar StructAccessNode aninhado
                    return self._generate_expression(StructAccessNode(parts[0], f"{parts[1]}.{parts[2]}"))
                else:
                    # Muito complexo - não suportado por enquanto
                    self._semantic_error(f"Acesso muito complexo a struct '{node.name}' não suportado", node)
            else:
                self._semantic_error(f"Variável '{node.name}' não foi declarada", node)
            
        elif isinstance(node, CallNode):
            # Função embutida 'ord'
            if node.function_name == 'ord':
                arg = self._generate_expression(node.arguments[0])
                # Se o argumento já é um char (i8), apenas fazer zext para int
                if arg.type == self.char_type:
                    return self.builder.zext(arg, self.int_type)
                # Se é um ponteiro para char, carregar o primeiro caractere
                elif isinstance(arg.type, ir.PointerType) and arg.type.pointee == self.char_type:
                    first_char = self.builder.load(arg)
                    return self.builder.zext(first_char, self.int_type)
                else:
                    # Caso inesperado, tentar converter
                    return self.builder.zext(arg, self.int_type)
            # Verificar se é uma função externa de casting
            if node.function_name == 'to_str':
                # Determinar qual versão de to_str usar baseado no tipo do argumento
                if node.arguments:
                    arg_node = node.arguments[0]
                    
                    # Verificar se o argumento é um array
                    if isinstance(arg_node, IdentifierNode):
                        var_name = arg_node.name
                        if var_name in self.type_map:
                            var_type = self.type_map[var_name]
                            if isinstance(var_type, ArrayType):
                                # É um array, usar array_to_str
                                array_ptr = self._generate_expression(arg_node)
                                array_size = ir.Constant(self.int_type, var_type.size if var_type.size else 0)
                                
                                # Fazer cast do ponteiro para array para ponteiro para elemento
                                if isinstance(var_type.element_type, IntType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                                    return self.builder.call(self.array_to_str_int, [element_ptr, array_size])
                                elif isinstance(var_type.element_type, FloatType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.float_type.as_pointer())
                                    return self.builder.call(self.array_to_str_float, [element_ptr, array_size])
                                else:
                                    # Para outros tipos, usar to_str_int como fallback
                                    func = self.to_str_int
                            elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, ArrayType):
                                # É uma referência para array
                                array_ptr = self._generate_expression(arg_node)
                                array_size = ir.Constant(self.int_type, var_type.target_type.size if var_type.target_type.size else 0)
                                
                                # Fazer cast do ponteiro para array para ponteiro para elemento
                                if isinstance(var_type.target_type.element_type, IntType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                                    return self.builder.call(self.array_to_str_int, [element_ptr, array_size])
                                elif isinstance(var_type.target_type.element_type, FloatType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.float_type.as_pointer())
                                    return self.builder.call(self.array_to_str_float, [element_ptr, array_size])
                                else:
                                    # Para outros tipos, usar to_str_int como fallback
                                    func = self.to_str_int
                            else:
                                # Não é um array, usar to_str normal
                                arg = self._generate_expression(arg_node)
                                if isinstance(arg.type, ir.DoubleType):
                                    func = self.to_str_float
                                else:
                                    func = self.to_str_int
                        else:
                            # Variável não encontrada, usar to_str normal
                            arg = self._generate_expression(arg_node)
                            if isinstance(arg.type, ir.DoubleType):
                                func = self.to_str_float
                            else:
                                func = self.to_str_int
                    elif isinstance(arg_node, StructAccessNode):
                        # Verificar se é acesso a campo de array de struct
                        struct_name = arg_node.struct_name
                        field_name = arg_node.field_name
                        
                        # Determinar o tipo de struct baseado na variável
                        struct_type_name = None
                        if struct_name in self.type_map:
                            var_type = self.type_map[struct_name]
                            if isinstance(var_type, StructType):
                                struct_type_name = var_type.name
                            elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                                struct_type_name = var_type.target_type.name
                        
                        # Verificar se o campo é um array
                        if (struct_type_name and 
                            hasattr(self, 'struct_field_types') and 
                            struct_type_name in self.struct_field_types and 
                            field_name in self.struct_field_types[struct_type_name]):
                            field_type = self.struct_field_types[struct_type_name][field_name]
                            if isinstance(field_type, ArrayType):
                                # É um campo de array, usar array_to_str
                                array_ptr = self._generate_expression(arg_node)
                                array_size = ir.Constant(self.int_type, field_type.size if field_type.size else 0)
                                
                                # Fazer cast do ponteiro para array para ponteiro para elemento
                                if isinstance(field_type.element_type, IntType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.int_type.as_pointer())
                                    return self.builder.call(self.array_to_str_int, [element_ptr, array_size])
                                elif isinstance(field_type.element_type, FloatType):
                                    element_ptr = self.builder.bitcast(array_ptr, self.float_type.as_pointer())
                                    return self.builder.call(self.array_to_str_float, [element_ptr, array_size])
                                else:
                                    # Para outros tipos, usar to_str_int como fallback
                                    func = self.to_str_int
                            else:
                                # Não é um array, usar to_str normal
                                arg = self._generate_expression(arg_node)
                                if isinstance(arg.type, ir.DoubleType):
                                    func = self.to_str_float
                                else:
                                    func = self.to_str_int
                        else:
                            # Não conseguiu determinar ou não é array, usar to_str normal
                            arg = self._generate_expression(arg_node)
                            if isinstance(arg.type, ir.DoubleType):
                                func = self.to_str_float
                            else:
                                func = self.to_str_int
                    else:
                        # Argumento não é um identificador nem acesso a struct, usar to_str normal
                        arg = self._generate_expression(arg_node)
                        if isinstance(arg.type, ir.DoubleType):
                            func = self.to_str_float
                        else:
                            func = self.to_str_int
                else:
                    func = self.to_str_int  # default
                
                # Gerar argumentos para to_str
                args = []
                for arg_node in node.arguments:
                    arg_value = self._generate_expression(arg_node)
                    
                    # Verificar se o argumento precisa de cast para o tipo esperado pela função
                    if func == self.to_str_int and arg_value.type != self.int_type:
                        if hasattr(arg_value.type, 'width') and arg_value.type.width == 1:
                            # Converter de i1 (bool) para i64 usando zext
                            arg_value = self.builder.zext(arg_value, self.int_type)
                        elif hasattr(arg_value.type, 'width') and arg_value.type.width == 8:
                            # Converter de i8 para i64
                            arg_value = self.builder.sext(arg_value, self.int_type)
                        elif arg_value.type != self.int_type:
                            # Para outros tipos, fazer bitcast
                            arg_value = self.builder.bitcast(arg_value, self.int_type)
                    elif func == self.to_str_float and arg_value.type != self.float_type:
                        # Converter para float se necessário
                        if arg_value.type == self.int_type:
                            arg_value = self.builder.sitofp(arg_value, self.float_type)
                        else:
                            arg_value = self.builder.bitcast(arg_value, self.float_type)
                    
                    args.append(arg_value)
                
                return self.builder.call(func, args)
            elif node.function_name == 'array_to_str':
                # Função array_to_str para converter arrays para string
                if not node.arguments or len(node.arguments) < 2:
                    raise NameError("Função 'array_to_str' requer dois argumentos: array e tamanho")
                
                array_arg = self._generate_expression(node.arguments[0])
                size_arg = self._generate_expression(node.arguments[1])
                
                # Determinar o tipo do array baseado no primeiro argumento
                if isinstance(node.arguments[0], IdentifierNode):
                    var_name = node.arguments[0].name
                    if var_name in self.type_map:
                        var_type = self.type_map[var_name]
                        if isinstance(var_type, ArrayType):
                            # Fazer cast do ponteiro para array para ponteiro para elemento
                            if isinstance(var_type.element_type, IntType):
                                element_ptr = self.builder.bitcast(array_arg, self.int_type.as_pointer())
                                return self.builder.call(self.array_to_str_int, [element_ptr, size_arg])
                            elif isinstance(var_type.element_type, FloatType):
                                element_ptr = self.builder.bitcast(array_arg, self.float_type.as_pointer())
                                return self.builder.call(self.array_to_str_float, [element_ptr, size_arg])
                        elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, ArrayType):
                            # Fazer cast do ponteiro para array para ponteiro para elemento
                            if isinstance(var_type.target_type.element_type, IntType):
                                element_ptr = self.builder.bitcast(array_arg, self.int_type.as_pointer())
                                return self.builder.call(self.array_to_str_int, [element_ptr, size_arg])
                            elif isinstance(var_type.target_type.element_type, FloatType):
                                element_ptr = self.builder.bitcast(array_arg, self.float_type.as_pointer())
                                return self.builder.call(self.array_to_str_float, [element_ptr, size_arg])
                
                # Fallback: assumir que é um array de int
                element_ptr = self.builder.bitcast(array_arg, self.int_type.as_pointer())
                return self.builder.call(self.array_to_str_int, [element_ptr, size_arg])
            elif node.function_name == 'to_int':
                func = self.to_int
            elif node.function_name == 'to_float':
                func = self.to_float
            elif node.function_name == 'length':
                # Função length para obter tamanho de arrays
                if not node.arguments:
                    raise NameError("Função 'length' requer um argumento")
                
                arg = self._generate_expression(node.arguments[0])
                
                # Verificar se o argumento é um array
                if isinstance(node.arguments[0], IdentifierNode):
                    var_name = node.arguments[0].name
                    if var_name in self.type_map:
                        var_type = self.type_map[var_name]
                        if isinstance(var_type, ArrayType):
                            # É um array, retornar o tamanho
                            array_type = var_type
                            if array_type.size is not None:
                                return ir.Constant(self.int_type, array_type.size)
                            else:
                                # Array sem tamanho definido, retornar 0
                                return ir.Constant(self.int_type, 0)
                        elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, ArrayType):
                            # É uma referência para array, retornar o tamanho do array alvo
                            array_type = var_type.target_type
                            if array_type.size is not None:
                                return ir.Constant(self.int_type, array_type.size)
                            else:
                                # Array sem tamanho definido, retornar 0
                                return ir.Constant(self.int_type, 0)
                        else:
                            # Não é um array, retornar 0
                            return ir.Constant(self.int_type, 0)
                    else:
                        # Não é um array, retornar 0
                        return ir.Constant(self.int_type, 0)
                else:
                    # Argumento não é um identificador, retornar 0
                    return ir.Constant(self.int_type, 0)
            elif node.function_name in self.functions:
                func = self.functions[node.function_name]
            elif node.function_name in self.imported_symbols:
                # Função importada (já foi declarada durante a inicialização)
                # Para funções com namespace (module.function), usar apenas o nome da função
                symbol_info = self.imported_symbols[node.function_name]
                if symbol_info['type'] == 'function':
                    # Extrair o nome simples da função para buscar na lista de funções declaradas
                    simple_function_name = node.function_name.split('.')[-1]
                    func = self.functions[simple_function_name]
                else:
                    self._semantic_error(f"'{node.function_name}' foi importado mas não é uma função", node)
            else:
                self._semantic_error(f"Função '{node.function_name}' não foi declarada", node)
            args = []
            
            # Gerar argumentos considerando tipos esperados
            for i, arg_node in enumerate(node.arguments):
                if i < len(func.args):
                    expected_type = func.args[i].type
                    
                    # Gerar o argumento
                    arg_value = self._generate_expression(arg_node, expected_type)
                    args.append(arg_value)
                else:
                    # Sem informação de tipo, gerar normalmente
                    args.append(self._generate_expression(arg_node))
            
            # Verificar se há arrays estáticos sendo passados para funções que esperam ponteiros
            args = self._convert_array_args_for_function_call(func, args)
            
            # Aplicar context manager para capturar erros de tipo mismatch
            with self._with_context(node):
                return self.builder.call(func, args)
            
        elif isinstance(node, BinaryOpNode):
            left = self._generate_expression(node.left)
            right = self._generate_expression(node.right)
            
            # Verificar se é operação com floats
            is_float_op = isinstance(left.type, ir.DoubleType) or isinstance(right.type, ir.DoubleType)
            
            # Converter operandos se necessário
            if is_float_op:
                if isinstance(left.type, ir.IntType):
                    left = self.builder.sitofp(left, self.float_type)
                if isinstance(right.type, ir.IntType):
                    right = self.builder.sitofp(right, self.float_type)
            
            # Operações aritméticas
            if node.operator == TokenType.PLUS:
                # Verificar se é concatenação de strings (pelo menos um lado é string)
                left_is_string = (isinstance(left.type, ir.PointerType) and left.type.pointee == self.char_type) or left.type == self.string_type
                right_is_string = (isinstance(right.type, ir.PointerType) and right.type.pointee == self.char_type) or right.type == self.string_type
                
                if left_is_string or right_is_string:
                    # Concatenação de strings - ambos os lados devem ser strings
                    if not left_is_string or not right_is_string:
                        raise TypeError("Operação + para strings requer que ambos os operandos sejam strings. Use to_str() para converter números para string.")
                    
                    # Calcular tamanho necessário
                    len1 = self.builder.call(self.strlen, [left])
                    len2 = self.builder.call(self.strlen, [right])
                    total_len = self.builder.add(len1, len2)
                    total_len = self.builder.add(total_len, ir.Constant(self.int_type, 1))  # +1 para null terminator
                    
                    # Alocar memória para resultado
                    result = self.builder.call(self.malloc, [total_len])
                    self._track_allocation(result)
                    
                    # Copiar primeira string
                    self.builder.call(self.strcpy, [result, left])
                    
                    # Concatenar segunda string
                    self.builder.call(self.strcat, [result, right])
                    
                    return result
                elif is_float_op:
                    return self.builder.fadd(left, right, name="fadd")
                else:
                    return self.builder.add(left, right, name="add")
            elif node.operator == TokenType.MINUS:
                if is_float_op:
                    return self.builder.fsub(left, right, name="fsub")
                else:
                    return self.builder.sub(left, right, name="sub")
            elif node.operator == TokenType.MULTIPLY:
                if is_float_op:
                    return self.builder.fmul(left, right, name="fmul")
                else:
                    return self.builder.mul(left, right, name="mul")
            elif node.operator == TokenType.DIVIDE:
                if is_float_op:
                    return self.builder.fdiv(left, right, name="fdiv")
                else:
                    return self.builder.sdiv(left, right, name="div")
            elif node.operator == TokenType.MODULO:
                if is_float_op:
                    return self.builder.call(self.fmod, [left, right], name="fmod")
                else:
                    return self.builder.srem(left, right, name="mod")
                
            # Comparações
            elif node.operator == TokenType.GT:
                if is_float_op:
                    return self.builder.fcmp_ordered('>', left, right, name="fgt")
                else:
                    return self.builder.icmp_signed('>', left, right, name="gt")
            elif node.operator == TokenType.LT:
                if is_float_op:
                    return self.builder.fcmp_ordered('<', left, right, name="flt")
                else:
                    return self.builder.icmp_signed('<', left, right, name="lt")
            elif node.operator == TokenType.GTE:
                if is_float_op:
                    return self.builder.fcmp_ordered('>=', left, right, name="fgte")
                else:
                    return self.builder.icmp_signed('>=', left, right, name="gte")
            elif node.operator == TokenType.LTE:
                if is_float_op:
                    return self.builder.fcmp_ordered('<=', left, right, name="flte")
                else:
                    return self.builder.icmp_signed('<=', left, right, name="lte")
            elif node.operator == TokenType.EQ or node.operator == TokenType.NEQ:
                # Verificar se é comparação com null (ponteiro)
                if (isinstance(left.type, ir.PointerType) and isinstance(right, ir.Constant) and right.constant is None) or \
                   (isinstance(right.type, ir.PointerType) and isinstance(left, ir.Constant) and left.constant is None):
                    # Comparação de ponteiro com null - comparar ponteiros diretamente
                    if node.operator == TokenType.EQ:
                        return self.builder.icmp_signed('==', left, right, name="eq")
                    else:
                        return self.builder.icmp_signed('!=', left, right, name="neq")
                
                # Verificar se ambos são ponteiros para char (strings)
                left_is_string = isinstance(left.type, ir.PointerType) and left.type.pointee == self.char_type
                right_is_string = isinstance(right.type, ir.PointerType) and right.type.pointee == self.char_type
                
                if left_is_string and right_is_string:
                    # Comparação de strings usando strcmp
                    cmp_result = self.builder.call(self.strcmp, [left, right])
                    zero = ir.Constant(ir.IntType(32), 0)
                    if node.operator == TokenType.EQ:
                        return self.builder.icmp_signed('==', cmp_result, zero, name="eq")
                    else:
                        return self.builder.icmp_signed('!=', cmp_result, zero, name="neq")
                
                # Para comparações de char individuais, garantir que ambos são i8
                if left_is_string and not right_is_string:
                    left = self.builder.load(left)
                if right_is_string and not left_is_string:
                    right = self.builder.load(right)
                
                # Para comparações de char (i8), usar icmp sem sinal
                if left.type == self.char_type and right.type == self.char_type:
                    if node.operator == TokenType.EQ:
                        return self.builder.icmp_unsigned('==', left, right, name="eq")
                    else:
                        return self.builder.icmp_unsigned('!=', left, right, name="neq")
                elif is_float_op:
                    if node.operator == TokenType.EQ:
                        return self.builder.fcmp_ordered('==', left, right, name="feq")
                    else:
                        return self.builder.fcmp_ordered('!=', left, right, name="fneq")
                else:
                    if node.operator == TokenType.EQ:
                        # Se qualquer lado for bool, converter ambos para bool
                        if left.type == self.bool_type or right.type == self.bool_type:
                            if left.type != self.bool_type:
                                left = self.builder.icmp_ne(left, ir.Constant(left.type, 0))
                            if right.type != self.bool_type:
                                right = self.builder.icmp_ne(right, ir.Constant(right.type, 0))
                        return self.builder.icmp_signed('==', left, right, name="eq")
                    else:
                        return self.builder.icmp_signed('!=', left, right, name="neq")
                
            # Operadores lógicos
            elif node.operator == TokenType.AND:
                # Converter para boolean se necessário (apenas se não for já bool)
                if left.type != self.bool_type:
                    left = self.builder.icmp_ne(left, ir.Constant(left.type, 0))
                if right.type != self.bool_type:
                    right = self.builder.icmp_ne(right, ir.Constant(right.type, 0))
                
                # Implementação simples do AND lógico (sem curto-circuito por enquanto)
                return self.builder.and_(left, right, name="and")
            elif node.operator == TokenType.OR:
                # Converter para boolean se necessário (apenas se não for já bool)
                if left.type != self.bool_type:
                    left = self.builder.icmp_ne(left, ir.Constant(left.type, 0))
                if right.type != self.bool_type:
                    right = self.builder.icmp_ne(right, ir.Constant(right.type, 0))
                
                # Implementação simples do OR lógico
                result = self.builder.or_(left, right, name="or")
                return result
                
        elif isinstance(node, UnaryOpNode):
            operand = self._generate_expression(node.operand)
            
            if node.operator == TokenType.NOT:
                # Converter para boolean se necessário
                if operand.type != self.bool_type:
                    operand = self.builder.icmp_ne(operand, ir.Constant(operand.type, 0))
                return self.builder.not_(operand, name="not")
            else:
                raise NotImplementedError(f"Operador unário não implementado: {node.operator}")
        
        elif isinstance(node, StructConstructorNode):
            return self._generate_struct_constructor(node, expected_type)
        
        elif isinstance(node, StringCharAccessNode):
            # Acesso a caractere de string literal: "hello"[1]
            # Gerar o índice
            index = self._generate_expression(node.index)
            
            # Verificar se o índice está dentro dos limites da string
            string_length = len(node.string)
            
            # Verificar se o índice está dentro dos limites
            if (hasattr(index, 'constant') and 
                isinstance(index.constant, int) and 
                0 <= index.constant < string_length):
                # Índice constante e válido
                char_value = ord(node.string[index.constant])
                char_str = self.builder.call(self.char_to_str, [ir.Constant(self.char_type, char_value)])
                return char_str
            else:
                # Índice dinâmico - usar string literal diretamente
                # Para strings literais, podemos acessar diretamente o caractere
                # e converter para string usando char_to_str
                
                # Criar um array temporário com os caracteres da string
                char_array = [ord(c) for c in node.string]
                char_array_type = ir.ArrayType(self.char_type, string_length)
                char_array_constant = ir.Constant(char_array_type, char_array)
                
                # Obter o ponteiro para o caractere no índice
                char_ptr = self.builder.gep(char_array_constant, [
                    ir.Constant(ir.IntType(32), 0),
                    index
                ])
                
                # Carregar o caractere
                char_value = self.builder.load(char_ptr)
                
                # Chamar char_to_str para converter o caractere para string
                char_str = self.builder.call(self.char_to_str, [char_value])
                return char_str
        
        raise NotImplementedError(f"Tipo de nó não implementado: {type(node)}")

    def _generate_struct_assignment(self, node: StructAssignmentNode):
        """Gerar código para atribuição de campo de struct: struct.campo = valor"""
        # Procurar a variável struct primeiro localmente, depois globalmente
        if node.struct_name in self.local_vars:
            struct_ptr = self.local_vars[node.struct_name]
            # Verificar se é um parâmetro de função (ponteiro para ponteiro)
            if isinstance(struct_ptr, ir.Argument):
                # É um parâmetro de função, precisa dereferenciar
                struct_ptr = self.builder.load(struct_ptr)
            elif (node.struct_name in self.type_map and 
                  isinstance(self.type_map[node.struct_name], ReferenceType)):
                # É uma referência, precisa dereferenciar
                struct_ptr = self.builder.load(struct_ptr)
            # NOVO: Se for um alloca (variável local), fazer load
            import llvmlite.ir.instructions
            if isinstance(struct_ptr, llvmlite.ir.instructions.AllocaInstr):
                struct_ptr = self.builder.load(struct_ptr)
        else:
            # Procurar nas variáveis globais (mapa interno primeiro)
            if node.struct_name in self.global_vars:
                struct_ptr = self.global_vars[node.struct_name]
            else:
                struct_ptr = self.module.globals.get(node.struct_name)
                if struct_ptr is None:
                    raise NameError(f"Struct '{node.struct_name}' não encontrado")
            # Se a global armazena ponteiro para struct (Node**), carregar para obter Node*
            from llvmlite import ir as _ir
            if isinstance(struct_ptr.type, _ir.PointerType) and isinstance(struct_ptr.type.pointee, _ir.PointerType):
                struct_ptr = self.builder.load(struct_ptr)
        
        # Determinar o tipo de struct baseado na variável
        struct_type_name = None
        if node.struct_name in self.type_map:
            var_type = self.type_map[node.struct_name]
            if isinstance(var_type, StructType):
                struct_type_name = var_type.name
            elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                struct_type_name = var_type.target_type.name
        
        if not struct_type_name or struct_type_name not in self.struct_fields:
            raise NameError(f"Struct '{struct_type_name}' não encontrado")
        
        # Verificar se o campo existe
        if node.field_name not in self.struct_fields[struct_type_name]:
            self._semantic_error(f"Campo '{node.field_name}' não encontrado em struct '{struct_type_name}'", node)
        
        # Ajustar ponteiro base e fazer cast para o tipo correto do struct
        if struct_type_name in self.struct_types:
            struct_type = self.struct_types[struct_type_name]
            # Se ainda for ponteiro para ponteiro, carregar primeiro (ex.: global Node**)
            if isinstance(struct_ptr.type, ir.PointerType) and isinstance(struct_ptr.type.pointee, ir.PointerType):
                struct_ptr = self.builder.load(struct_ptr)
            struct_ptr = self.builder.bitcast(struct_ptr, struct_type.as_pointer())
        
        # Obter o índice do campo
        field_index = self.struct_fields[struct_type_name][node.field_name]
        
        # Obter o tipo do campo
        field_type = None
        if hasattr(self, 'struct_field_types') and struct_type_name in self.struct_field_types:
            field_type = self.struct_field_types[struct_type_name][node.field_name]
        
        # Acessar o campo - garantir que struct_ptr seja um ponteiro válido
        if not isinstance(struct_ptr.type, ir.PointerType):
            raise TypeError(f"Esperado ponteiro para struct, recebido: {struct_ptr.type}")
        
        field_ptr = self.builder.gep(struct_ptr, [
            ir.Constant(ir.IntType(32), 0),
            ir.Constant(ir.IntType(32), field_index)
        ])
        
        # Gerar o valor a ser atribuído
        if field_type:
            llvm_field_type = self._convert_type(field_type)
            value = self._generate_expression(node.value, llvm_field_type)
        else:
            value = self._generate_expression(node.value)
        
        # Tratar casos especiais
        if isinstance(field_type, ReferenceType):
            # Para campos de referência, garantir que o valor seja um ponteiro nulo se for null
            if isinstance(value.type, ir.IntType) and value.type.width == 64:
                # Se o valor é null (i64), converter para ponteiro nulo
                value = ir.Constant(field_ptr.type.pointee, None)
            elif isinstance(value.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.PointerType):
                # Se ambos são ponteiros, fazer cast se necessário
                if value.type != field_ptr.type.pointee:
                    value = self.builder.bitcast(value, field_ptr.type.pointee)
        
        # Armazenar o valor
        self.builder.store(value, field_ptr)

    def _generate_nested_struct_assignment(self, node: NestedStructAssignmentNode):
        """Gerar código para atribuição aninhada de struct: struct.campo.subcampo = valor"""
        # Procurar a variável struct primeiro localmente, depois globalmente
        if node.struct_name in self.local_vars:
            struct_ptr = self.local_vars[node.struct_name]
            # Verificar se é um parâmetro de função (ponteiro para ponteiro)
            if isinstance(struct_ptr, ir.Argument):
                # É um parâmetro de função, precisa dereferenciar
                struct_ptr = self.builder.load(struct_ptr)
            elif (node.struct_name in self.type_map and 
                  isinstance(self.type_map[node.struct_name], ReferenceType)):
                # É uma referência, precisa dereferenciar
                struct_ptr = self.builder.load(struct_ptr)
            else:
                # É uma variável local que é um ponteiro para struct, precisa dereferenciar
                struct_ptr = self.builder.load(struct_ptr)
        else:
            # Procurar nas variáveis globais (preferir mapa interno)
            if node.struct_name in self.global_vars:
                struct_ptr = self.global_vars[node.struct_name]
            else:
                struct_ptr = self.module.globals.get(node.struct_name)
                if struct_ptr is None:
                    raise NameError(f"Struct '{node.struct_name}' não encontrado")
            # Globais armazenam ponteiro para struct (ex.: Node**). Carregar para obter Node*
            from llvmlite import ir as _ir
            if isinstance(struct_ptr.type, _ir.PointerType) and isinstance(struct_ptr.type.pointee, _ir.PointerType):
                struct_ptr = self.builder.load(struct_ptr)
        
        # Determinar o tipo de struct baseado na variável
        struct_type_name = None
        if node.struct_name in self.type_map:
            var_type = self.type_map[node.struct_name]
            if isinstance(var_type, StructType):
                struct_type_name = var_type.name
            elif isinstance(var_type, ReferenceType) and isinstance(var_type.target_type, StructType):
                struct_type_name = var_type.target_type.name
        
        if not struct_type_name or struct_type_name not in self.struct_fields:
            raise NameError(f"Struct '{struct_type_name}' não encontrado")
        
        # Ajustar ponteiro base e fazer cast para o tipo correto do struct
        if struct_type_name in self.struct_types:
            struct_type = self.struct_types[struct_type_name]
            # Se ainda for ponteiro para ponteiro, carregar primeiro (ex.: global Node**)
            if isinstance(struct_ptr.type, ir.PointerType) and isinstance(struct_ptr.type.pointee, ir.PointerType):
                struct_ptr = self.builder.load(struct_ptr)
            struct_ptr = self.builder.bitcast(struct_ptr, struct_type.as_pointer())
        
        # Navegar pelo caminho dos campos
        current_ptr = struct_ptr
        current_struct_type = struct_type_name
        
        for i, field_name in enumerate(node.field_path):
            # Verificar se o campo existe no struct atual
            if current_struct_type not in self.struct_fields or field_name not in self.struct_fields[current_struct_type]:
                raise NameError(f"Campo '{field_name}' não encontrado em struct '{current_struct_type}'")
            
            # Obter o índice do campo
            field_index = self.struct_fields[current_struct_type][field_name]
            
            # Acessar o campo - garantir que current_ptr seja um ponteiro válido
            if not isinstance(current_ptr.type, ir.PointerType):
                raise TypeError(f"Esperado ponteiro para struct, recebido: {current_ptr.type}")
            
            field_ptr = self.builder.gep(current_ptr, [
                ir.Constant(ir.IntType(32), 0),
                ir.Constant(ir.IntType(32), field_index)
            ])
            
            # Se não é o último campo, continuar navegando
            if i < len(node.field_path) - 1:
                # Obter o tipo do campo para determinar o próximo struct
                if hasattr(self, 'struct_field_types') and current_struct_type in self.struct_field_types:
                    field_type = self.struct_field_types[current_struct_type][field_name]
                    
                    if isinstance(field_type, StructType):
                        # Campo é um struct embutido: 'field_ptr' já é um ponteiro para o struct literal
                        if field_type.name in self.struct_types:
                            field_struct_type = self.struct_types[field_type.name]
                            # Navegar usando o próprio ponteiro do campo (sem alocar, sem null-check)
                            current_ptr = field_ptr
                            current_struct_type = field_type.name
                        else:
                            raise NameError(f"Struct '{field_type.name}' não encontrado")
                    elif isinstance(field_type, ReferenceType) and isinstance(field_type.target_type, StructType):
                        # O campo é uma referência para struct
                        if field_type.target_type.name in self.struct_types:
                            field_struct_type = self.struct_types[field_type.target_type.name]
                            # Carregar o valor do campo (que deve ser um ponteiro)
                            field_value = self.builder.load(field_ptr)
                            
                            # Verificar se é null
                            null_ptr = ir.Constant(field_ptr.type.pointee, None)
                            is_null = self.builder.icmp_unsigned('==', field_value, null_ptr)
                            
                            # Criar blocos para if/else
                            then_block = self.builder.function.append_basic_block('then')
                            else_block = self.builder.function.append_basic_block('else')
                            merge_block = self.builder.function.append_basic_block('merge')
                            
                            # Branch baseado na condição
                            self.builder.cbranch(is_null, then_block, else_block)
                            
                            # Bloco then: criar novo struct
                            self.builder.position_at_end(then_block)
                            # Calcular tamanho correto via GEP(null, 1) e ptrtoint
                            null_ptr = ir.Constant(field_struct_type.as_pointer(), None)
                            one32 = ir.Constant(ir.IntType(32), 1)
                            size_ptr = self.builder.gep(null_ptr, [one32])
                            size_val = self.builder.ptrtoint(size_ptr, ir.IntType(64))
                            new_struct_ptr = self.builder.call(self.malloc, [size_val])
                            self._track_allocation(new_struct_ptr)
                            
                            # Fazer cast para o tipo correto
                            new_struct_ptr = self.builder.bitcast(new_struct_ptr, field_struct_type.as_pointer())
                            
                            # Armazenar o novo struct no campo
                            # Fazer cast para i8* antes de armazenar
                            new_struct_ptr_as_i8 = self.builder.bitcast(new_struct_ptr, ir.IntType(8).as_pointer())
                            self.builder.store(new_struct_ptr_as_i8, field_ptr)
                            
                            # Usar o novo struct para continuar navegando
                            current_ptr = new_struct_ptr
                            
                            # Branch para o bloco de merge
                            self.builder.branch(merge_block)
                            
                            # Bloco else: usar struct existente
                            self.builder.position_at_end(else_block)
                            # Se não é null, fazer cast e continuar
                            current_ptr = self.builder.bitcast(field_value, field_struct_type.as_pointer())
                            
                            # Branch para o bloco de merge
                            self.builder.branch(merge_block)
                            
                            # Bloco de merge: continuar com o ponteiro correto
                            self.builder.position_at_end(merge_block)
                            
                            # Definir current_ptr baseado no bloco de onde veio (phi)
                            phi_current_ptr = self.builder.phi(field_struct_type.as_pointer())
                            phi_current_ptr.add_incoming(new_struct_ptr, then_block)
                            phi_current_ptr.add_incoming(current_ptr, else_block)
                            current_ptr = phi_current_ptr
                            
                            current_struct_type = field_type.target_type.name
                        else:
                            raise NameError(f"Struct '{field_type.target_type.name}' não encontrado")
                    else:
                        raise NameError(f"Campo '{field_name}' não é um struct")
                else:
                    # Fallback: assumir que o campo é um struct com o mesmo nome
                    current_ptr = field_ptr
                    current_struct_type = field_name
            else:
                # Último campo, atribuir o valor
                # Obter o tipo do campo
                field_type = None
                if hasattr(self, 'struct_field_types') and current_struct_type in self.struct_field_types:
                    field_type = self.struct_field_types[current_struct_type][field_name]
                
                # Gerar o valor a ser atribuído
                if field_type:
                    llvm_field_type = self._convert_type(field_type)
                    value = self._generate_expression(node.value, llvm_field_type)
                else:
                    value = self._generate_expression(node.value)
                
                # Tratar casos especiais
                if isinstance(field_type, ReferenceType):
                    # Para campos de referência, garantir que o valor seja um ponteiro nulo se for null
                    if isinstance(value.type, ir.IntType) and value.type.width == 64:
                        # Se o valor é null (i64), converter para ponteiro nulo
                        value = ir.Constant(field_ptr.type.pointee, None)
                    elif isinstance(value.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.PointerType):
                        # Se ambos são ponteiros, fazer cast se necessário
                        if value.type != field_ptr.type.pointee:
                            value = self.builder.bitcast(value, field_ptr.type.pointee)
                else:
                    # Para campos que não são referências, garantir type match exato
                    if hasattr(value, 'type') and hasattr(field_ptr, 'type') and value.type != field_ptr.type.pointee:
                        if isinstance(value.type, ir.PointerType) and isinstance(field_ptr.type.pointee, ir.PointerType):
                                    value = self.builder.bitcast(value, field_ptr.type.pointee)
                
                # Campo final: armazenamento com casos especiais
                from llvmlite import ir as _ir
                # Se é um struct embutido e o valor veio como ponteiro para struct, carregar antes de armazenar
                if isinstance(field_type, StructType):
                    target_ty = field_ptr.type.pointee  # struct literal LLVM
                    if isinstance(value.type, _ir.PointerType) and isinstance(value.type.pointee, _ir.LiteralStructType):
                        # Ajustar tipo do ponteiro, se necessário
                        if value.type.pointee != target_ty:
                            value = self.builder.bitcast(value, target_ty.as_pointer())
                        loaded_struct = self.builder.load(value)
                        self.builder.store(loaded_struct, field_ptr)
                    else:
                        # Se já for o struct literal, armazenar diretamente
                        self.builder.store(value, field_ptr)
                else:
                    # Armazenamento padrão
                    self.builder.store(value, field_ptr)

    def _calculate_struct_size(self, struct_type: ir.LiteralStructType) -> int:
        """Calcula o tamanho correto de um struct usando as APIs do LLVM"""
        try:
            # Tentar usar a API do LLVM para calcular o tamanho
            target = llvm.Target.from_triple(self.triple)
            target_machine = target.create_target_machine()
            
            # Converter o tipo llvmlite para o tipo interno do LLVM
            # Isso é necessário porque get_abi_size espera um tipo interno
            llvm_type = struct_type._get_llvm_type()
            struct_size = target_machine.target_data.get_abi_size(llvm_type)
            return struct_size
        except Exception as e:
            # Fallback: cálculo manual mais robusto
            return self._calculate_struct_size_manual(struct_type)
    
    def _calculate_struct_size_manual(self, struct_type: ir.LiteralStructType) -> int:
        """Cálculo manual do tamanho do struct com alinhamento correto"""
        total_size = 0
        max_alignment = 1
        
        for field_type in struct_type.elements:
            # Calcular tamanho e alinhamento do campo
            if isinstance(field_type, ir.IntType):
                if field_type.width == 1:  # bool
                    field_size = 1
                    field_alignment = 1
                elif field_type.width <= 8:  # i8, i16, i32
                    field_size = field_type.width // 8
                    field_alignment = field_size
                else:  # i64 e maiores
                    field_size = 8
                    field_alignment = 8
            elif isinstance(field_type, ir.DoubleType):  # float/double
                field_size = 8
                field_alignment = 8
            elif isinstance(field_type, ir.PointerType):  # string/pointer
                field_size = 8
                field_alignment = 8
            elif isinstance(field_type, ir.ArrayType):  # Array dentro do struct
                # Para arrays, calcular o tamanho total dos elementos
                element_type = field_type.element
                if isinstance(element_type, ir.IntType):
                    if element_type.width <= 8:
                        element_size = max(1, element_type.width // 8)
                    else:
                        element_size = 8
                elif isinstance(element_type, ir.DoubleType):
                    element_size = 8
                elif isinstance(element_type, ir.PointerType):
                    element_size = 8
                else:
                    element_size = 8  # Fallback
                
                field_size = element_size * field_type.count
                field_alignment = 8  # Arrays são alinhados em 8 bytes
            elif isinstance(field_type, ir.LiteralStructType):  # struct aninhado
                field_size = self._calculate_struct_size_manual(field_type)
                field_alignment = 8  # Structs são alinhados em 8 bytes
            else:
                # Para tipos desconhecidos, assumir 8 bytes
                field_size = 8
                field_alignment = 8
            
            # Aplicar alinhamento do campo
            padding = (field_alignment - (total_size % field_alignment)) % field_alignment
            total_size += padding + field_size
            max_alignment = max(max_alignment, field_alignment)
        
        # Alinhamento final do struct
        final_padding = (max_alignment - (total_size % max_alignment)) % max_alignment
        return total_size + final_padding

    def _generate_struct_constructor(self, node: StructConstructorNode, expected_type: ir.Type = None) -> ir.Value:
        struct_name = node.struct_name
        if struct_name not in self.struct_types:
            # Verificar se é um struct importado
            if struct_name in self.imported_symbols:
                symbol_info = self.imported_symbols[struct_name]
                if symbol_info['type'] == 'struct':
                    # Gerar definição do struct importado se ainda não existir
                    struct_node = symbol_info['node']
                    self._process_struct_definition(struct_node)
                else:
                    raise NameError(f"'{struct_name}' foi importado mas não é um struct")
            else:
                raise NameError(f"Struct '{struct_name}' não definido")
        struct_type = self.struct_types[struct_name]
        
        # Calcular tamanho via GEP(null, 1) e ptrtoint para respeitar layout do LLVM
        null_ptr = ir.Constant(struct_type.as_pointer(), None)
        one = ir.Constant(ir.IntType(32), 1)
        size_ptr = self.builder.gep(null_ptr, [one])
        size = self.builder.ptrtoint(size_ptr, ir.IntType(64))
        size = self.builder.call(self.malloc, [size])
        self._track_allocation(size)
        struct_ptr = self.builder.bitcast(size, struct_type.as_pointer())
        struct_info = None
        # First, check local struct definitions
        for stmt in self.global_ast.statements:
            if isinstance(stmt, StructDefinitionNode) and stmt.name == struct_name:
                struct_info = stmt
                break
        
        # If not found locally, check imported structs
        if not struct_info:
            for symbol_name, symbol_info in self.imported_symbols.items():
                if (symbol_info['type'] == 'struct' and 
                    symbol_name.split('.')[-1] == struct_name):  # Handle namespaced imports
                    struct_info = symbol_info['node']
                    break
        
        if not struct_info:
            raise NameError(f"Definição do struct '{struct_name}' não encontrada")
        for i, (field_name, field_type) in enumerate(struct_info.fields):
            if i < len(node.arguments):
                llvm_field_type = self._convert_type(field_type)
                arg_value = self._generate_expression(node.arguments[i], llvm_field_type)
                # Garantir que struct_ptr seja um ponteiro válido
                if not isinstance(struct_ptr.type, ir.PointerType):
                    raise TypeError(f"Esperado ponteiro para struct, recebido: {struct_ptr.type}")
                
                field_ptr = self.builder.gep(struct_ptr, [
                    ir.Constant(ir.IntType(32), 0),
                    ir.Constant(ir.IntType(32), i)
                ])
                if isinstance(field_type, ReferenceType):
                    # Para campos de referência, garantir que o valor seja um ponteiro nulo se for null
                    if isinstance(arg_value.type, ir.IntType) and arg_value.type.width == 64:
                        # Se o valor é null (i64), converter para ponteiro nulo
                        arg_value = ir.Constant(llvm_field_type, None)
                elif isinstance(arg_value.type, ir.PointerType) and isinstance(llvm_field_type, ir.PointerType):
                    # Se ambos são ponteiros, fazer cast se necessário
                    if arg_value.type != llvm_field_type:
                        arg_value = self.builder.bitcast(arg_value, llvm_field_type)
                elif isinstance(arg_value.type, ir.LiteralStructType) and isinstance(llvm_field_type, ir.PointerType):
                    # Se o valor é um struct e o campo espera um ponteiro, não fazer cast
                    # O struct já é o tipo correto
                    pass
                elif isinstance(arg_value.type, ir.PointerType) and isinstance(llvm_field_type, ir.LiteralStructType):
                    # Se o valor é um ponteiro e o campo espera um struct, não fazer cast
                    # O ponteiro já é o tipo correto
                    pass
                
                # Caso especial: campo é array estático e argumento é ponteiro para array → copiar elementos
                target_ty = field_ptr.type.pointee
                from llvmlite import ir as _ir
                if isinstance(target_ty, _ir.ArrayType) and isinstance(arg_value.type, _ir.PointerType) and isinstance(arg_value.type.pointee, _ir.ArrayType):
                    zero32 = _ir.Constant(_ir.IntType(32), 0)
                    # dst: ponteiro para primeiro elemento do array no struct
                    dst_elem_ptr = self.builder.gep(field_ptr, [zero32, zero32], inbounds=True)
                    # src: ponteiro para primeiro elemento do array fonte
                    src_elem_ptr = self.builder.gep(arg_value, [zero32, zero32], inbounds=True)
                    for idx in range(target_ty.count):
                        idx_const = _ir.Constant(self.int_type, idx)
                        si = self.builder.gep(src_elem_ptr, [idx_const], inbounds=True)
                        di = self.builder.gep(dst_elem_ptr, [idx_const], inbounds=True)
                        self.builder.store(self.builder.load(si), di)
                    continue
                # Novo: campo é array estático e argumento é ponteiro para elemento → copiar elementos
                if isinstance(target_ty, _ir.ArrayType) and isinstance(arg_value.type, _ir.PointerType) and arg_value.type.pointee == target_ty.element:
                    zero32 = _ir.Constant(_ir.IntType(32), 0)
                    dst_elem_ptr = self.builder.gep(field_ptr, [zero32, zero32], inbounds=True)
                    for idx in range(target_ty.count):
                        idx_const = _ir.Constant(self.int_type, idx)
                        si = self.builder.gep(arg_value, [idx_const], inbounds=True)
                        di = self.builder.gep(dst_elem_ptr, [idx_const], inbounds=True)
                        self.builder.store(self.builder.load(si), di)
                    continue

                # Novo: se o campo é um struct embutido e o argumento veio como ponteiro para struct,
                # carregamos o valor do ponteiro (fazendo bitcast se o pointee divergir) e armazenamos por valor.
                if isinstance(target_ty, _ir.LiteralStructType):
                    if isinstance(arg_value.type, _ir.PointerType) and isinstance(arg_value.type.pointee, _ir.LiteralStructType):
                        if arg_value.type.pointee != target_ty:
                            arg_value = self.builder.bitcast(arg_value, target_ty.as_pointer())
                        loaded_struct = self.builder.load(arg_value)
                        self.builder.store(loaded_struct, field_ptr)
                        continue
                if arg_value.type != target_ty:
                    if isinstance(arg_value.type, ir.PointerType) and isinstance(target_ty, ir.PointerType):
                        arg_value = self.builder.bitcast(arg_value, target_ty)
                    elif isinstance(arg_value.type, ir.IntType) and isinstance(target_ty, ir.IntType):
                        if arg_value.type.width < target_ty.width:
                            arg_value = self.builder.sext(arg_value, target_ty)
                        elif arg_value.type.width > target_ty.width:
                            arg_value = self.builder.trunc(arg_value, target_ty)
                    else:
                        # Como fallback, não converter e deixar o LLVM apontar erro se houver
                        pass
                
                self.builder.store(arg_value, field_ptr)
        return struct_ptr

# Função para executar código via JIT
def execute_ir(llvm_ir: str):
    """Executa o código LLVM IR usando JIT compilation"""
    # No Windows, configurar console para UTF-8
    if sys.platform == "win32":
        import subprocess
        # Configurar code page para UTF-8
        subprocess.run(["chcp", "65001"], shell=True, capture_output=True)
        # Configurar variável de ambiente
        import os
        os.environ['PYTHONIOENCODING'] = 'utf-8'
    
    # Criar engine de execução
    llvm.initialize()
    llvm.initialize_native_target()
    llvm.initialize_native_asmprinter()
    
    # Parse do módulo
    mod = llvm.parse_assembly(llvm_ir)
    mod.verify()
    
    # Criar engine JIT
    target = llvm.Target.from_default_triple()
    target_machine = target.create_target_machine()
    engine = llvm.create_mcjit_compiler(mod, target_machine)
    
    # Adicionar funções da biblioteca C
    if sys.platform == "win32":
        # No Windows, carregar as bibliotecas necessárias
        llvm.load_library_permanently("msvcrt.dll")
        llvm.load_library_permanently("kernel32.dll")
    else:
        llvm.load_library_permanently("libc.so.6")
    
    # Executar função main
    main_ptr = engine.get_function_address("main")
    
    # Criar tipo de função para ctypes
    import ctypes
    main_func = ctypes.CFUNCTYPE(ctypes.c_int)(main_ptr)
    
    # Executar
    result = main_func()
    return result

# Sistema de módulos
class ModuleManager:
    def __init__(self):
        self.loaded_modules: Dict[str, Dict[str, Any]] = {}
        self.module_paths: List[str] = [".", "std", "noxy_examples"]  # Diretórios de busca
    
    def load_module(self, module_name: str) -> Dict[str, Any]:
        """Carrega um módulo e retorna suas funções e variáveis exportadas"""
        if module_name in self.loaded_modules:
            return self.loaded_modules[module_name]
        
        # Buscar arquivo do módulo
        module_file = self._find_module_file(module_name)
        if not module_file:
            raise NoxyError(f"Módulo '{module_name}' não encontrado")
        
        # Parsear o módulo
        try:
            with open(module_file, 'r', encoding='utf-8') as f:
                source = f.read()
            
            # Parsear o módulo sem criar um novo compilador (evitar circular import)
            lexer = Lexer(source)
            tokens = lexer.tokenize()
            parser = Parser(tokens)
            ast = parser.parse()
            
            # Extrair funções e variáveis globais
            exported_symbols = self._extract_exported_symbols(ast)
            self.loaded_modules[module_name] = exported_symbols
            
            return exported_symbols
            
        except Exception as e:
            raise NoxyError(f"Erro ao carregar módulo '{module_name}': {str(e)}")
    
    def _find_module_file(self, module_name: str) -> Optional[str]:
        """Encontra o arquivo do módulo nos diretórios de busca (suporta packages)"""
        # Suporte para packages aninhados (ex: utils.math -> utils/math.nx)
        module_parts = module_name.split('.')
        
        for path in self.module_paths:
            # Caso 1: Arquivo direto (module.nx)
            if len(module_parts) == 1:
                module_file = os.path.join(path, f"{module_name}.nx")
                if os.path.exists(module_file):
                    return module_file
            
            # Caso 2: Package aninhado (utils.math -> utils/math.nx)
            package_path = os.path.join(path, *module_parts[:-1])
            module_file = os.path.join(package_path, f"{module_parts[-1]}.nx")
            if os.path.exists(module_file):
                return module_file
            
            # Caso 3: Package com __init__.nx (utils -> utils/__init__.nx)
            if len(module_parts) == 1:
                package_dir = os.path.join(path, module_name)
                init_file = os.path.join(package_dir, "__init__.nx")
                if os.path.exists(init_file):
                    return init_file
            
            # Caso 4: Subpackage com __init__.nx (utils.math -> utils/math/__init__.nx)
            package_path = os.path.join(path, *module_parts)
            init_file = os.path.join(package_path, "__init__.nx")
            if os.path.exists(init_file):
                return init_file
        
        return None
    
    def _extract_exported_symbols(self, ast: ProgramNode) -> Dict[str, Any]:
        """Extrai símbolos exportáveis de um AST (funções e variáveis globais)"""
        symbols = {}
        
        for stmt in ast.statements:
            if isinstance(stmt, FunctionNode):
                symbols[stmt.name] = {
                    'type': 'function',
                    'node': stmt,
                    'params': stmt.params,
                    'return_type': stmt.return_type
                }
            elif isinstance(stmt, AssignmentNode) and stmt.is_global:
                symbols[stmt.identifier] = {
                    'type': 'variable',
                    'node': stmt,
                    'var_type': stmt.var_type
                }
            elif isinstance(stmt, StructDefinitionNode):
                symbols[stmt.name] = {
                    'type': 'struct',
                    'node': stmt,
                    'fields': stmt.fields
                }
        
        return symbols

# Compilador principal
class NoxyCompiler:
    def __init__(self):
        self.lexer = None
        self.parser = None
        self.codegen = None
        self.module_manager = ModuleManager()
        self.imported_symbols = {}  # Cache de símbolos importados
    
    def _process_imports(self, ast: ProgramNode):
        """Processa todas as instruções de import antes da análise semântica"""
        for stmt in ast.statements:
            if isinstance(stmt, UseNode):
                self._handle_use_statement(stmt)
    
    def _handle_use_statement(self, use_node: UseNode):
        """Processa uma instrução use específica"""
        try:
            # Carregar o módulo
            module_symbols = self.module_manager.load_module(use_node.module_name)

            
            if use_node.import_all:
                # Import all: use module select *
                for symbol_name, symbol_info in module_symbols.items():
                    self.imported_symbols[symbol_name] = symbol_info
            elif use_node.selected_symbols:
                # Import specific: use module select symbol1, symbol2
                # Primeiro, coletar todos os símbolos solicitados e suas dependências
                symbols_to_import = set()
                for symbol_name in use_node.selected_symbols:
                    if symbol_name in module_symbols:
                        # Não adicionar ainda - deixar _collect_dependencies fazer isso
                        # Adicionar dependências recursivamente
                        self._collect_dependencies(symbol_name, module_symbols, symbols_to_import)
                    else:
                        raise NoxyError(f"Símbolo '{symbol_name}' não encontrado no módulo '{use_node.module_name}'")
                

                
                # Importar todos os símbolos necessários (explícitos + dependências)
                for symbol_name in symbols_to_import:
                    self.imported_symbols[symbol_name] = module_symbols[symbol_name]
            else:
                # Import whole module: use module
                # Criar namespace - apenas permitir acesso via module.symbol
                for symbol_name, symbol_info in module_symbols.items():
                    prefixed_name = f"{use_node.module_name}.{symbol_name}"
                    self.imported_symbols[prefixed_name] = symbol_info
                        
        except Exception as e:
            raise NoxyError(f"Erro ao processar import: {str(e)}")

    def _collect_dependencies(self, symbol_name: str, module_symbols: Dict[str, Any], collected: set):
        """Coleta recursivamente todas as dependências de um símbolo"""
        if symbol_name in collected or symbol_name not in module_symbols:
            return
        
        collected.add(symbol_name)
        symbol_info = module_symbols[symbol_name]
        
        if symbol_info['type'] == 'function':
            # Analisar o corpo da função para encontrar dependências
            func_node = symbol_info['node']
            dependencies = self._analyze_function_dependencies(func_node)
            
            for dep in dependencies:
                if dep in module_symbols and dep not in collected:
                    self._collect_dependencies(dep, module_symbols, collected)
        elif symbol_info['type'] == 'struct':
            # Para structs, não há dependências internas para analisar
            # mas ainda precisamos garantir que seja adicionado ao conjunto
            pass
    
    def _analyze_function_dependencies(self, func_node) -> set:
        """Analisa uma função para encontrar todas as funções e variáveis que ela usa"""
        dependencies = set()
        
        def visit_node(node):
            if node is None:
                return
            
            if hasattr(node, '__class__'):
                class_name = node.__class__.__name__
                
                if class_name == 'IdentifierNode':
                    # Adicionar identificadores que podem ser dependências
                    dependencies.add(node.name)
                elif class_name == 'CallNode':
                    # Adicionar chamadas de função
                    dependencies.add(node.function_name)
                    # Visitar argumentos
                    for arg in node.arguments:
                        visit_node(arg)
                elif class_name == 'StructConstructorNode':
                    # Adicionar construtor de struct
                    dependencies.add(node.struct_name)
                    # Visitar argumentos
                    for arg in node.arguments:
                        visit_node(arg)
                elif class_name == 'AssignmentNode':
                    # Visitar valor da atribuição
                    if hasattr(node, 'value') and node.value:
                        visit_node(node.value)
                elif class_name == 'BinaryOpNode':
                    # Visitar operandos
                    visit_node(node.left)
                    visit_node(node.right)
                elif class_name == 'ArrayAccessNode':
                    # Adicionar nome do array e visitar índice
                    dependencies.add(node.array_name)
                    visit_node(node.index)
                elif class_name == 'IfNode':
                    # Visitar condição e blocos
                    visit_node(node.condition)
                    for stmt in node.then_branch:
                        visit_node(stmt)
                    if hasattr(node, 'else_branch') and node.else_branch:
                        for stmt in node.else_branch:
                            visit_node(stmt)
                elif class_name == 'WhileNode':
                    # Visitar condição e corpo
                    visit_node(node.condition)
                    for stmt in node.body:
                        visit_node(stmt)
                elif class_name == 'ReturnNode':
                    # Visitar valor de retorno
                    if node.value:
                        visit_node(node.value)
                elif hasattr(node, '__dict__'):
                    # Visitar todos os atributos do nó recursivamente
                    for attr_name, attr_value in node.__dict__.items():
                        if isinstance(attr_value, list):
                            for item in attr_value:
                                visit_node(item)
                        elif attr_value is not None and attr_name not in ['line', 'column']:
                            visit_node(attr_value)
        
        # Visitar o corpo da função
        for stmt in func_node.body:
            visit_node(stmt)
        
        # Filtrar built-ins e identificadores que claramente não são funções
        filtered_dependencies = set()
        builtin_functions = {'printf', 'malloc', 'free', 'strlen', 'strcpy', 'strcat', 'to_str', 'array_to_str', 'to_int', 'to_float', 'ord', 'length', 'print', 'to_str'}
        
        for dep in dependencies:
            # Não incluir built-ins nem parâmetros da função
            param_names = {param[0] for param in func_node.params} if hasattr(func_node, 'params') else set()
            if dep not in builtin_functions and dep not in param_names:
                filtered_dependencies.add(dep)
        
        return filtered_dependencies
    
    def _perform_semantic_analysis(self, ast: ProgramNode):
        """Realiza análise semântica para detectar erros de tipo antes da geração de código"""
        self._process_imports(ast)  # Processar imports primeiro
        self._check_function_return_types(ast)
        self._check_fstring_expressions(ast)
    
    def _check_function_return_types(self, ast: ProgramNode):
        """Verifica se os tipos de retorno das funções estão corretos"""
        for stmt in ast.statements:
            if isinstance(stmt, FunctionNode):
                self._validate_function_returns(stmt)
    
    def _validate_function_returns(self, func_node: FunctionNode):
        """Valida se os returns de uma função são compatíveis com seu tipo de retorno declarado"""
        expected_return_type = func_node.return_type
        
        # Encontrar todos os statements de return na função
        return_statements = self._find_return_statements(func_node.body)
        
        for return_stmt in return_statements:
            if isinstance(expected_return_type, VoidType):
                # Função void não deve retornar valores
                if return_stmt.value is not None:
                    self._semantic_error_for_return(
                        f"Função '{func_node.name}' é declarada como 'void' mas está tentando retornar um valor. "
                        f"Adicione '-> {self._suggest_return_type(return_stmt.value)}' após os parâmetros ou remova o valor de retorno.",
                        return_stmt
                    )
            else:
                # Função com tipo de retorno específico deve retornar valores
                if return_stmt.value is None:
                    self._semantic_error_for_return(
                        f"Função '{func_node.name}' é declarada para retornar '{self._type_to_string(expected_return_type)}' "
                        f"mas este return não possui valor. Use 'return <valor>' ou declare a função como '-> void'.",
                        return_stmt
                    )
                else:
                    # TODO: Aqui podemos adicionar verificação de compatibilidade de tipos específicos
                    # Por enquanto, apenas verificamos se há valor quando esperado
                    pass
    
    def _find_return_statements(self, statements: List[ASTNode]) -> List[ReturnNode]:
        """Encontra recursivamente todos os statements de return em uma lista de statements"""
        returns = []
        for stmt in statements:
            if isinstance(stmt, ReturnNode):
                returns.append(stmt)
            elif isinstance(stmt, IfNode):
                returns.extend(self._find_return_statements(stmt.then_branch))
                if stmt.else_branch:
                    returns.extend(self._find_return_statements(stmt.else_branch))
            elif isinstance(stmt, WhileNode):
                returns.extend(self._find_return_statements(stmt.body))
            # Adicionar outros tipos de statements que podem conter returns aninhados
        return returns
    
    def _suggest_return_type(self, return_value: ASTNode) -> str:
        """Sugere um tipo de retorno baseado no valor sendo retornado"""
        if isinstance(return_value, NumberNode):
            return "int"
        elif isinstance(return_value, FloatNode):
            return "float"
        elif isinstance(return_value, StringNode):
            return "string"
        elif isinstance(return_value, BooleanNode):
            return "bool"
        elif isinstance(return_value, IdentifierNode):
            # Para identificadores, tentar inferir o tipo baseado no contexto
            return self._infer_identifier_type(return_value.name)
        elif isinstance(return_value, StructAccessNode):
            # Para acesso a campos de struct, tentar inferir baseado no campo
            return self._infer_struct_field_type(return_value)
        elif isinstance(return_value, BinaryOpNode):
            # Para operações binárias, inferir baseado no tipo da operação
            return self._infer_binary_op_type(return_value)
        elif isinstance(return_value, CallNode):
            # Para chamadas de função, verificar o tipo de retorno da função
            return self._infer_function_call_type(return_value)
        else:
            return "tipo_apropriado"
    
    def _infer_identifier_type(self, identifier_name: str) -> str:
        """Infere o tipo de um identificador baseado em heurísticas de nome"""
        # Heurísticas baseadas em nomes comuns
        if any(keyword in identifier_name.lower() for keyword in ['count', 'size', 'length', 'index', 'hash', 'value', 'i', 'j', 'k']):
            return "int"
        elif any(keyword in identifier_name.lower() for keyword in ['name', 'key', 'text', 'str', 'message']):
            return "string"
        elif any(keyword in identifier_name.lower() for keyword in ['found', 'valid', 'present', 'ok', 'flag']):
            return "bool"
        elif any(keyword in identifier_name.lower() for keyword in ['price', 'rate', 'percent', 'float']):
            return "float"
        else:
            # Fallback: assumir string para casos não identificados
            return "string"
    
    def _infer_struct_field_type(self, struct_access: StructAccessNode) -> str:
        """Infere o tipo de um campo de struct baseado em heurísticas"""
        field_name = struct_access.field_name.lower()
        if any(keyword in field_name for keyword in ['key', 'name', 'value', 'text', 'str']):
            return "string"
        elif any(keyword in field_name for keyword in ['count', 'size', 'index', 'id']):
            return "int"
        elif any(keyword in field_name for keyword in ['found', 'valid', 'active']):
            return "bool"
        else:
            return "string"  # Fallback comum para campos de struct
    
    def _infer_binary_op_type(self, binary_op: BinaryOpNode) -> str:
        """Infere o tipo de uma operação binária"""
        # Operações aritméticas geralmente retornam int ou float
        if binary_op.operator in [TokenType.PLUS, TokenType.MINUS, TokenType.MULTIPLY, TokenType.DIVIDE, TokenType.MOD]:
            # Se qualquer operando é float, resultado é float
            left_type = self._suggest_return_type(binary_op.left)
            right_type = self._suggest_return_type(binary_op.right)
            if left_type == "float" or right_type == "float":
                return "float"
            else:
                return "int"
        # Operações de comparação retornam bool
        elif binary_op.operator in [TokenType.EQ, TokenType.NEQ, TokenType.LT, TokenType.GT, TokenType.LTE, TokenType.GTE]:
            return "bool"
        # Operações lógicas retornam bool
        elif binary_op.operator in [TokenType.AND, TokenType.OR]:
            return "bool"
        # Concatenação de strings
        elif binary_op.operator == TokenType.CONCAT:
            return "string"
        else:
            return "int"  # Fallback para operações aritméticas
    
    def _infer_function_call_type(self, call: CallNode) -> str:
        """Infere o tipo de retorno de uma chamada de função baseado no nome"""
        func_name = call.function_name.lower()
        
        # Funções conhecidas do sistema
        if func_name in ['strlen', 'to_int', 'ord']:
            return "int"
        elif func_name in ['to_float']:
            return "float"
        elif func_name in ['to_str', 'to_str_int', 'to_str_float', 'char_to_str']:
            return "string"
        
        # Heurísticas baseadas em nomes
        elif any(keyword in func_name for keyword in ['get', 'find', 'search']) and any(keyword in func_name for keyword in ['key', 'value', 'name', 'text']):
            return "string"
        elif any(keyword in func_name for keyword in ['count', 'size', 'length', 'hash', 'index']):
            return "int"
        elif any(keyword in func_name for keyword in ['is', 'has', 'check', 'valid']):
            return "bool"
        else:
            return "tipo_apropriado"
    
    def _type_to_string(self, type_obj: Type) -> str:
        """Converte um objeto Type para sua representação string"""
        if isinstance(type_obj, IntType):
            return "int"
        elif isinstance(type_obj, FloatType):
            return "float"
        elif isinstance(type_obj, StringType):
            return "string"
        elif isinstance(type_obj, BoolType):
            return "bool"
        elif isinstance(type_obj, VoidType):
            return "void"
        elif isinstance(type_obj, ArrayType):
            return f"{self._type_to_string(type_obj.element_type)}[{type_obj.size or ''}]"
        else:
            return "unknown"
    
    def _check_fstring_expressions(self, ast: ProgramNode):
        """Verifica se as expressões dentro das f-strings são válidas"""
        self._traverse_and_check_fstrings(ast)
    
    def _traverse_and_check_fstrings(self, node):
        """Percorre recursivamente o AST procurando por f-strings"""
        if isinstance(node, FStringNode):
            self._validate_fstring_parts(node)
        elif isinstance(node, ProgramNode):
            for stmt in node.statements:
                self._traverse_and_check_fstrings(stmt)
        elif isinstance(node, FunctionNode):
            for stmt in node.body:
                self._traverse_and_check_fstrings(stmt)
        elif isinstance(node, IfNode):
            self._traverse_and_check_fstrings(node.condition)
            for stmt in node.then_branch:
                self._traverse_and_check_fstrings(stmt)
            if node.else_branch:
                for stmt in node.else_branch:
                    self._traverse_and_check_fstrings(stmt)
        elif isinstance(node, WhileNode):
            self._traverse_and_check_fstrings(node.condition)
            for stmt in node.body:
                self._traverse_and_check_fstrings(stmt)
        elif isinstance(node, BinaryOpNode):
            self._traverse_and_check_fstrings(node.left)
            self._traverse_and_check_fstrings(node.right)
        elif isinstance(node, UnaryOpNode):
            self._traverse_and_check_fstrings(node.operand)
        elif isinstance(node, AssignmentNode):
            self._traverse_and_check_fstrings(node.value)
        elif isinstance(node, PrintNode):
            self._traverse_and_check_fstrings(node.expression)
        elif isinstance(node, ReturnNode) and node.value:
            self._traverse_and_check_fstrings(node.value)
        elif isinstance(node, CallNode):
            for arg in node.arguments:
                self._traverse_and_check_fstrings(arg)
        # Adicionar outros tipos de nós conforme necessário
    
    def _validate_fstring_parts(self, fstring_node: FStringNode):
        """Valida as partes de uma f-string"""
        for part in fstring_node.parts:
            if not isinstance(part, str):
                # É uma expressão - verificar se é válida
                # Para agora, apenas verificamos se não é None
                if part is None:
                    if hasattr(fstring_node, 'line') and hasattr(fstring_node, 'column'):
                        source_line = None
                        if self.lexer and hasattr(self.lexer, 'source_lines') and fstring_node.line <= len(self.lexer.source_lines):
                            source_line = self.lexer.source_lines[fstring_node.line - 1]
                        raise NoxySemanticError("Expressão inválida em f-string", fstring_node.line, fstring_node.column, source_line)
                    else:
                        raise NoxySemanticError("Expressão inválida em f-string")
    
    def _semantic_error_for_return(self, message: str, node: ASTNode):
        """Lança erro semântico específico para problemas de return com informações de contexto"""
        if hasattr(node, 'line') and hasattr(node, 'column') and node.line is not None and node.column is not None:
            source_line = None
            if self.lexer and hasattr(self.lexer, 'source_lines') and node.line <= len(self.lexer.source_lines):
                source_line = self.lexer.source_lines[node.line - 1]
            raise NoxySemanticError(message, node.line, node.column, source_line)
        else:
            raise NoxySemanticError(message)
        
    def compile(self, source: str) -> str:
        try:
            # Análise léxica
            self.lexer = Lexer(source)
            tokens = self.lexer.tokenize()
            
            # Análise sintática
            self.parser = Parser(tokens, self.lexer.source_lines)
            ast = self.parser.parse()
            
            # Análise semântica
            self._perform_semantic_analysis(ast)
            
            # Geração de código
            self.codegen = LLVMCodeGenerator(self.lexer.source_lines, self.imported_symbols)
            llvm_module = self.codegen.generate(ast)
            
            return str(llvm_module)
        except NoxyError:
            # Re-lançar erros Noxy sem modificação
            raise
        except Exception as e:
            # Capturar outros erros e convertê-los em NoxyError
            raise NoxyError(f"Erro interno do compilador: {str(e)}")

    def compile_with_debug_ir(self, source: str) -> tuple[str, str]:
        """
        Compila o código e retorna uma tupla (ir_code, error_message).
        Tenta gerar IR mesmo quando há erros, para auxiliar no debug.
        """
        ir_code = None
        error_message = None
        
        try:
            # Análise léxica
            self.lexer = Lexer(source)
            tokens = self.lexer.tokenize()
            
            # Análise sintática
            self.parser = Parser(tokens, self.lexer.source_lines)
            ast = self.parser.parse()
            
            # Tentar análise semântica
            try:
                self._perform_semantic_analysis(ast)
                semantic_success = True
            except NoxyError as e:
                # Capturar erro semântico mas continuar
                error_message = f"ERRO SEMÂNTICO: {str(e)}"
                semantic_success = False
            
            # Tentar geração de código mesmo com erros semânticos
            try:
                self.codegen = LLVMCodeGenerator(self.lexer.source_lines, self.imported_symbols)
                
                # Tentar gerar IR mesmo se houver erros durante a geração
                try:
                    llvm_module = self.codegen.generate(ast)
                    ir_code = str(llvm_module)
                except Exception as gen_error:
                    # Se a geração falhou completamente, tentar gerar IR parcial
                    # verificando se já foi criado algum módulo
                    if hasattr(self.codegen, 'module') and self.codegen.module:
                        try:
                            ir_code = str(self.codegen.module)
                        except:
                            ir_code = None
                    
                    # Registrar o erro de geração
                    if error_message:
                        error_message += f"\nErro adicional na geração de código: {str(gen_error)}"
                    else:
                        error_message = f"ERRO DE GERAÇÃO DE CÓDIGO: {str(gen_error)}"
                
                # Se só houve erro semântico, retornar sucesso parcial
                if not semantic_success and ir_code:
                    return ir_code, error_message
                    
            except Exception as codegen_error:
                # Se a criação do gerador de código falhou
                if error_message:
                    error_message += f"\nErro na inicialização do gerador de código: {str(codegen_error)}"
                else:
                    error_message = f"ERRO DE GERAÇÃO DE CÓDIGO: {str(codegen_error)}"
            
            # Se chegou aqui sem erro_message, compilação foi bem-sucedida
            if not error_message:
                return ir_code, None
            else:
                return ir_code, error_message
                
        except NoxySyntaxError as e:
            error_message = f"ERRO DE SINTAXE: {str(e)}"
            return ir_code, error_message
        except Exception as e:
            error_message = f"ERRO INTERNO DO COMPILADOR: {str(e)}"
            return ir_code, error_message
    
    def compile_to_object(self, source: str, output_file: str):
        try:
            print(f"Gerando IR LLVM...")
            # Gerar IR LLVM
            llvm_ir = self.compile(source)
            
            print(f"Configurando target...")
            # Configurar target
            llvm.initialize()
            llvm.initialize_native_target()
            llvm.initialize_native_asmprinter()
            
            # Obter o triple correto para a plataforma e ajustar para MinGW/GCC quando necessário
            default_triple = llvm.get_default_triple()
            if default_triple.endswith("-pc-windows-msvc"):
                triple = default_triple.replace("-pc-windows-msvc", "-w64-windows-gnu")
            else:
                triple = default_triple
            print(f"Triple: {triple}")
            
            # Criar target
            target = llvm.Target.from_triple(triple)
            print(f"Target criado: {target}")
            
            # Criar target machine com configurações apropriadas
            print("Configurando target machine para Windows...")
            # Usar config estática para evitar GOT/_GLOBAL_OFFSET_TABLE_ com GCC/MinGW
            try:
                target_machine = target.create_target_machine(reloc='static', codemodel='large', opt=2)
            except TypeError:
                target_machine = target.create_target_machine(opt=2)
            
            print(f"Target machine criada: {target_machine}")
            
            print("Parseando assembly...")
            # Compilar para objeto
            try:
                mod = llvm.parse_assembly(llvm_ir)
            except RuntimeError as e:
                # Melhorar mensagem de erro LLVM
                error_msg = str(e)
                if "LLVM IR parsing error" in error_msg:
                    # Extrair informações da linha do erro LLVM
                    import re
                    line_match = re.search(r'<string>:(\d+):', error_msg)
                    if line_match:
                        llvm_line = int(line_match.group(1))
                        # Tentar mapear de volta para o código fonte
                        # (isso é uma aproximação, pois o mapeamento exato seria complexo)
                        raise NoxyCodeGenError(
                            f"Erro na geração de código LLVM: {error_msg}\n"
                            f"Isso pode ser causado por um problema no código Noxy, como:\n"
                            f"- Acesso incorreto a campos de struct\n"
                            f"- Tipos incompatíveis em expressões\n"
                            f"- Uso de variáveis não declaradas"
                        )
                raise NoxyCodeGenError(f"Erro na geração de código LLVM: {error_msg}")
            
            print("Verificando módulo...")
            try:
                mod.verify()
            except Exception as e:
                raise NoxyCodeGenError(f"Código LLVM inválido gerado: {str(e)}")
            
            # Definir o triple e data layout
            mod.triple = triple
            mod.data_layout = str(target_machine.target_data)
            
            print("Otimizando...")
            # Otimizar
            pmb = llvm.create_pass_manager_builder()
            pmb.opt_level = 2
            pm = llvm.create_module_pass_manager()
            pmb.populate(pm)
            pm.run(mod)
            
            print(f"Gerando código objeto em '{output_file}'...")
            # Gerar código objeto
            object_data = target_machine.emit_object(mod)
            print(f"Tamanho do objeto gerado: {len(object_data)} bytes")
            
            with open(output_file, 'wb') as f:
                f.write(object_data)
            
            print(f"Arquivo objeto criado com sucesso: {output_file}")
            
        except NoxyError as e:
            print(f"Erro de compilação: {e}")
            raise
        except Exception as e:
            print(f"Erro interno na geração do objeto: {e}")
            import traceback
            traceback.print_exc()
            raise NoxyCodeGenError(f"Erro interno na geração do objeto: {str(e)}")

def read_source_file(file_path: str) -> str:
    """Lê o conteúdo de um arquivo de código fonte."""
    try:
        with open(file_path, 'r', encoding='utf-8') as f:
            return f.read()
    except FileNotFoundError:
        print(f"Erro: Arquivo '{file_path}' não encontrado.")
        sys.exit(1)
    except Exception as e:
        print(f"Erro ao ler arquivo '{file_path}': {e}")
        sys.exit(1)

def print_usage():
    """Imprime informações de uso do programa."""
    print("Noxy Compiler")
    print("Uso:")
    print("  python compiler.py <arquivo.nx>                    # Executar programa")
    print("  python compiler.py --compile <arquivo.nx>          # Gerar arquivo objeto")
    print("  python compiler.py --help                          # Mostrar esta ajuda")
    print("")
    print("Exemplos:")
    print("  python compiler.py programa.nx")
    print("  python compiler.py --compile programa.nx")

# Exemplo de uso
if __name__ == "__main__":
    import sys
    
    # Verificar argumentos
    if len(sys.argv) < 2 or "--help" in sys.argv or "-h" in sys.argv:
        print_usage()
        sys.exit(0)
    
    # Determinar modo de operação
    compile_mode = "--compile" in sys.argv
    source_file = None
    
    if compile_mode:
        # Modo compilação: --compile <arquivo>
        if len(sys.argv) < 3:
            print("Erro: Arquivo de entrada não especificado para modo --compile")
            print_usage()
            sys.exit(1)
        source_file = sys.argv[2]
    else:
        # Modo execução: <arquivo>
        source_file = sys.argv[1]
    
    # Ler arquivo de código fonte
    print(f"Lendo arquivo: {source_file}")
    source_code = read_source_file(source_file)
    
    # Compilar
    compiler = NoxyCompiler()
    
    try:
        # Tentar gerar IR LLVM com debug ativado
        llvm_ir, error_message = compiler.compile_with_debug_ir(source_code)
        
        # Sempre mostrar o LLVM IR se foi gerado, mesmo com erros
        if llvm_ir:
            print("=== LLVM IR Gerado ===")
            print(llvm_ir)
        
        # Se houve erro, mostrar e parar se necessário
        if error_message:
            print(f"\n{error_message}")
            if not llvm_ir:
                print("(IR LLVM não pôde ser gerado devido aos erros)")
            else:
                print("(IR LLVM gerado parcialmente para debug)")
            sys.exit(1)
        
        # Se chegou aqui, compilação foi bem-sucedida
        if compile_mode:
            # Modo compilação: gerar arquivo objeto
            output_file = "output.obj" if sys.platform == "win32" else "output.o"
            compiler.compile_to_object(source_code, output_file)
            print(f"\nCódigo objeto gerado em '{output_file}'")
            
            if sys.platform == "win32":
                print("\nPara criar executável no Windows:")
                print("1. Com MinGW: gcc -mcmodel=large output.obj -o programa.exe")
                print("2. Com Clang: clang output.obj -o programa.exe")
                print("3. Com MSVC: cl output.obj /Fe:programa.exe")
            else:
                print("\nPara criar executável: gcc output.o -o programa")
        else:
            # Modo execução: executar usando JIT
            print("\n=== Executando o programa ===")
            try:
                execute_ir(llvm_ir)
            except Exception as e:
                print(f"Erro na execução JIT: {e}")
                import traceback
                traceback.print_exc()
        
    except Exception as e:
        print(f"ERRO INTERNO DO COMPILADOR:")
        print(f"{e}")
        print("\nPor favor, reporte este erro aos desenvolvedores.")
        import traceback
        traceback.print_exc()
        sys.exit(1)
