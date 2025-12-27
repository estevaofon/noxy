package ast

import (
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
	Size        *int // nil for dynamic/slice, pointer for fixed size? Or simple int
}

func (t *ArrayType) String() string {
	size := ""
	if t.Size != nil {
		size = fmt.Sprintf("%d", *t.Size)
	}
	return fmt.Sprintf("%s[%s]", t.ElementType.String(), size)
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
