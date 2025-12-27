package ast

import (
	"bytes"
	"fmt"
	"noxy-vm/internal/token"
	"strings"
)

type Node interface {
	TokenLiteral() string
	String() string
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

// PROGRAM
type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

func (p *Program) String() string {
	var out string
	for _, s := range p.Statements {
		out += s.String()
	}
	return out
}

// TYPES
type NoxyType interface {
	String() string
}

type PrimitiveType struct {
	Name string
}

func (t *PrimitiveType) String() string { return t.Name }

type ArrayType struct {
	ElementType NoxyType
	Size        int // 0 for dynamic
}

func (at *ArrayType) String() string { return at.ElementType.String() + "[]" }

type MapType struct {
	KeyType   NoxyType
	ValueType NoxyType
}

func (mt *MapType) String() string {
	return "map[" + mt.KeyType.String() + ", " + mt.ValueType.String() + "]"
}

// STATEMENTS

type AssignStmt struct {
	Token  token.Token // the '=' token
	Target Expression  // Identifier or struct field
	Value  Expression
}

func (as *AssignStmt) statementNode()       {}
func (as *AssignStmt) TokenLiteral() string { return as.Token.Literal }
func (as *AssignStmt) String() string {
	return fmt.Sprintf("%s = %s", as.Target.String(), as.Value.String())
}

type LetStmt struct {
	Token token.Token // The 'let' token
	Name  *Identifier
	Type  NoxyType
	Value Expression
}

func (ls *LetStmt) statementNode()       {}
func (ls *LetStmt) TokenLiteral() string { return ls.Token.Literal }
func (ls *LetStmt) String() string {
	out := fmt.Sprintf("%s %s: %s", ls.TokenLiteral(), ls.Name.String(), ls.Type.String())
	if ls.Value != nil {
		out += " = " + ls.Value.String()
	}
	return out
}

type ReturnStmt struct {
	Token       token.Token // The 'return' token
	ReturnValue Expression
}

func (rs *ReturnStmt) statementNode()       {}
func (rs *ReturnStmt) TokenLiteral() string { return rs.Token.Literal }
func (rs *ReturnStmt) String() string {
	out := rs.TokenLiteral() + " "
	if rs.ReturnValue != nil {
		out += rs.ReturnValue.String()
	}
	return out
}

type BreakStmt struct {
	Token token.Token
}

func (bs *BreakStmt) statementNode()       {}
func (bs *BreakStmt) TokenLiteral() string { return bs.Token.Literal }
func (bs *BreakStmt) String() string       { return "break" }

type UseStmt struct {
	Token  token.Token // 'use'
	Module string
}

func (us *UseStmt) statementNode()       {}
func (us *UseStmt) TokenLiteral() string { return us.Token.Literal }
func (us *UseStmt) String() string       { return "use " + us.Module }

type ExpressionStmt struct {
	Token      token.Token // The first token of the expression
	Expression Expression
}

func (es *ExpressionStmt) statementNode()       {}
func (es *ExpressionStmt) TokenLiteral() string { return es.Token.Literal }
func (es *ExpressionStmt) String() string {
	if es.Expression != nil {
		return es.Expression.String()
	}
	return ""
}

// EXPRESSIONS

type Identifier struct {
	Token token.Token // The token.IDENTIFIER token
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }
func (i *Identifier) String() string       { return i.Value }

type IntegerLiteral struct {
	Token token.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }
func (il *IntegerLiteral) String() string       { return il.Token.Literal }

type FloatLiteral struct {
	Token token.Token
	Value float64
}

func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FloatLiteral) String() string       { return fl.Token.Literal }

type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StringLiteral) String() string       { return sl.Token.Literal }

type BytesLiteral struct {
	Token token.Token
	Value string // Or []byte. Token matches string literal logic in Lexer so probably string.
}

func (bl *BytesLiteral) expressionNode()      {}
func (bl *BytesLiteral) TokenLiteral() string { return bl.Token.Literal }
func (bl *BytesLiteral) String() string       { return bl.Token.Literal }

type NullLiteral struct {
	Token token.Token
}

func (n *NullLiteral) expressionNode()      {}
func (n *NullLiteral) TokenLiteral() string { return n.Token.Literal }
func (n *NullLiteral) String() string       { return "null" }

type ZerosLiteral struct {
	Token token.Token
	Size  Expression
}

func (z *ZerosLiteral) expressionNode()      {}
func (z *ZerosLiteral) TokenLiteral() string { return z.Token.Literal }
func (z *ZerosLiteral) String() string {
	return fmt.Sprintf("zeros(%s)", z.Size.String())
}

type Boolean struct {
	Token token.Token
	Value bool
}

func (b *Boolean) expressionNode()      {}
func (b *Boolean) TokenLiteral() string { return b.Token.Literal }
func (b *Boolean) String() string       { return b.Token.Literal }

type PrefixExpression struct {
	Token    token.Token // The prefix token, e.g. ! or -
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
func (pe *PrefixExpression) String() string {
	return "(" + pe.Operator + pe.Right.String() + ")"
}

type InfixExpression struct {
	Token    token.Token // The operator token, e.g. +
	Left     Expression
	Operator string
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *InfixExpression) String() string {
	return "(" + ie.Left.String() + " " + ie.Operator + " " + ie.Right.String() + ")"
}

// Basic implementation. Will add more nodes (Func, Struct, etc) as we progress.

type BlockStatement struct {
	Token      token.Token // the { token or similar
	Statements []Statement
}

func (bs *BlockStatement) statementNode()       {}
func (bs *BlockStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BlockStatement) String() string {
	var out string
	for _, s := range bs.Statements {
		out += s.String()
	}
	return out
}

type IfStatement struct {
	Token       token.Token // The 'if' token
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfStatement) statementNode()       {}
func (ie *IfStatement) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfStatement) String() string {
	out := "if " + ie.Condition.String() + " " + ie.Consequence.String()
	if ie.Alternative != nil {
		out += " else " + ie.Alternative.String()
	}
	return out
}

type WhileStatement struct {
	Token     token.Token // The 'while' token
	Condition Expression
	Body      *BlockStatement
}

func (ws *WhileStatement) statementNode()       {}
func (ws *WhileStatement) TokenLiteral() string { return ws.Token.Literal }
func (ws *WhileStatement) String() string {
	return "while " + ws.Condition.String() + " " + ws.Body.String()
}

type FunctionStatement struct {
	Token      token.Token // The 'func' token
	Name       string
	Parameters []*Identifier
	Body       *BlockStatement
}

func (fs *FunctionStatement) statementNode()       {}
func (fs *FunctionStatement) TokenLiteral() string { return fs.Token.Literal }
func (fs *FunctionStatement) String() string {
	var params []string
	for _, p := range fs.Parameters {
		params = append(params, p.String())
	}
	return "func " + fs.Name + "(" + strings.Join(params, ", ") + ") " + fs.Body.String()
}

type CallExpression struct {
	Token     token.Token // The '(' token
	Function  Expression  // Identifier or FunctionStatement
	Arguments []Expression
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }
func (ce *CallExpression) String() string {
	out := ce.Function.String() + "("
	for i, a := range ce.Arguments {
		out += a.String()
		if i < len(ce.Arguments)-1 {
			out += ", "
		}
	}
	out += ")"
	return out
}

type ArrayLiteral struct {
	Token    token.Token // The '[' token
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()      {}
func (al *ArrayLiteral) TokenLiteral() string { return al.Token.Literal }
func (al *ArrayLiteral) String() string {
	var out bytes.Buffer
	elements := []string{}
	for _, el := range al.Elements {
		elements = append(elements, el.String())
	}
	out.WriteString("[")
	out.WriteString(strings.Join(elements, ", "))
	out.WriteString("]")
	return out.String()
}

type MapLiteral struct {
	Token token.Token               // '{'
	Pairs map[Expression]Expression // Keys are expressions too
	// Since map range is random, for parsing we might want slice of pairs to maintain order or for deterministic tests?
	// But runtime map is unordered.
	// AST usually uses slice of keys and values to easier processing.
	Keys   []Expression
	Values []Expression
}

func (ml *MapLiteral) expressionNode()      {}
func (ml *MapLiteral) TokenLiteral() string { return ml.Token.Literal }
func (ml *MapLiteral) String() string {
	var out bytes.Buffer
	pairs := []string{}
	for i, key := range ml.Keys {
		pairs = append(pairs, key.String()+": "+ml.Values[i].String())
	}
	out.WriteString("{")
	out.WriteString(strings.Join(pairs, ", "))
	out.WriteString("}")
	return out.String()
}

type IndexExpression struct {
	Token token.Token // The '[' token
	Left  Expression
	Index Expression
}

func (ie *IndexExpression) expressionNode()      {}
func (ie *IndexExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IndexExpression) String() string {
	return "(" + ie.Left.String() + "[" + ie.Index.String() + "])"
}

type StructStatement struct {
	Token  token.Token // 'struct'
	Name   string
	Fields map[string]NoxyType // Or ordered list? Ordered for constructor?
	// Spec says: struct Point x: int, y: int end.
	// Constructor arguments order matters.
	// So we need list of fields.
	FieldsList []*StructField
}

type StructField struct {
	Name string
	Type NoxyType
}

func (ss *StructStatement) statementNode()       {}
func (ss *StructStatement) TokenLiteral() string { return ss.Token.Literal }
func (ss *StructStatement) String() string {
	s := "struct " + ss.Name + " "
	for i, f := range ss.FieldsList {
		s += f.Name + ": " + f.Type.String()
		if i < len(ss.FieldsList)-1 {
			s += ", "
		}
	}
	s += " end"
	return s
}

type MemberAccessExpression struct {
	Token  token.Token // '.'
	Left   Expression
	Member string // Identifier value
}

func (mae *MemberAccessExpression) expressionNode()      {}
func (mae *MemberAccessExpression) TokenLiteral() string { return mae.Token.Literal }
func (mae *MemberAccessExpression) String() string {
	return "(" + mae.Left.String() + "." + mae.Member + ")"
}
