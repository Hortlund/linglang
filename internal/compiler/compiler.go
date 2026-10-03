// Package compiler lowers a deliberately small Go-like language to Erlang.
package compiler

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

//go:embed runtime.erl
var Runtime string

//go:embed supervision.erl
var SupervisionRuntime string

// RuntimeSources returns a fresh file set for CLI, tests, and benchmark builds.
func RuntimeSources() map[string]string {
	return map[string]string{"linglang_rt.erl": Runtime, "linglang_sup.erl": SupervisionRuntime}
}

type compiler struct {
	fset         *token.FileSet
	info         *types.Info
	cells        map[types.Object]string
	next         int
	ret          string
	loops        []string
	breaks       []string
	values       map[types.Object]string
	optimizing   bool
	rooted       bool
	intrinsics   map[types.Object]string
	processTypes map[string]*types.Named
	rangeOps     map[ast.Expr]string
}

// Options retains the original cell-based lowering for comparisons and debugging.
type Options struct{ DisableOptimizations bool }

// SourceFile keeps each input's filename for parser and compiler diagnostics.
type SourceFile struct {
	Filename string
	Source   []byte
}

// Compile returns an Erlang module. Unsupported syntax is an error, never ignored.
func Compile(filename string, source []byte) (string, error) {
	return CompileWithOptions(filename, source, Options{})
}

func CompileWithOptions(filename string, source []byte, options Options) (string, error) {
	return CompileFilesWithOptions([]SourceFile{{Filename: filename, Source: source}}, options)
}

// CompileFiles compiles one package from multiple source files into one BEAM module.
func CompileFiles(sources []SourceFile) (string, error) {
	return CompileFilesWithOptions(sources, Options{})
}

func CompileFilesWithOptions(sources []SourceFile, options Options) (string, error) {
	return compileFiles(sources, options, nil)
}

// TestCase describes a zero-argument test in a _test.lang file.
type TestCase struct {
	Name     string
	Filename string
	Line     int
}

// CompileTestFilesWithOptions accepts packages without main and exports a test
// dispatcher. Discovery and lowering share the same parsed, checked package.
func CompileTestFilesWithOptions(sources []SourceFile, options Options) (string, []TestCase, error) {
	var tests []TestCase
	program, err := compileFiles(sources, options, &tests)
	return program, tests, err
}

func compileFiles(sources []SourceFile, options Options, tests *[]TestCase) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("no source files provided")
	}
	// Sort a copy: callers may supply files in any order, and keep their inputs.
	sources = append([]SourceFile(nil), sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Filename < sources[j].Filename })
	fset := token.NewFileSet()
	c := newCompiler(fset)
	var files []*ast.File
	for i, source := range sources {
		if i > 0 && source.Filename == sources[i-1].Filename {
			return "", fmt.Errorf("duplicate source file %q", source.Filename)
		}
		file, err := parser.ParseFile(fset, source.Filename, source.Source, 0)
		if err != nil {
			return "", err
		}
		if file.Name.Name != "main" {
			return "", c.errorf(file.Name, "only package main is supported")
		}
		if len(file.Imports) != 0 {
			return "", c.errorf(file.Imports[0], "imports are not supported yet")
		}
		files = append(files, file)
	}
	config := typeConfig()
	prelude, err := c.prelude()
	if err != nil {
		return "", err
	}
	pkg, err := config.Check("main", fset, append([]*ast.File{prelude}, files...), c.info)
	if err != nil {
		return "", err
	}
	c.registerIntrinsics(prelude)
	// Lower the complete package through the existing IR. The original nodes
	// retain their positions and go/types identities across file boundaries.
	file := &ast.File{Package: files[0].Package, Name: files[0].Name}
	for _, input := range files {
		file.Decls = append(file.Decls, input.Decls...)
	}
	if err := c.validateProcessSyntax(file); err != nil {
		return "", err
	}
	main, ok := pkg.Scope().Lookup("main").(*types.Func)
	if !ok && tests == nil {
		return "", c.errorf(file, "a func main() entry point is required")
	}
	if ok {
		sig := main.Type().(*types.Signature)
		if sig.Params().Len() != 0 || sig.Results().Len() != 0 {
			return "", fmt.Errorf("%s: main must have no parameters or results", c.fset.Position(main.Pos()))
		}
	}
	if tests != nil {
		for i, input := range files {
			if !strings.HasSuffix(sources[i].Filename, "_test.lang") {
				continue
			}
			for _, decl := range input.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				sig := c.info.Defs[fn.Name].Type().(*types.Signature)
				if fn.Recv != nil || fn.Type.TypeParams != nil || sig.Params().Len() != 0 || sig.Results().Len() != 0 {
					return "", c.errorf(fn, "test functions must have no receiver, type parameters, parameters, or results")
				}
				position := c.fset.Position(fn.Name.Pos())
				*tests = append(*tests, TestCase{Name: fn.Name.Name, Filename: position.Filename, Line: position.Line})
			}
		}
		sort.Slice(*tests, func(i, j int) bool { return (*tests)[i].Name < (*tests)[j].Name })
	}
	// Check every variable and constant, including unused declarations and fields.
	var validationErr error
	ast.Inspect(file, func(n ast.Node) bool {
		if validationErr != nil {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := c.info.Defs[id].(*types.Var); ok && !c.supportedType(v.Type(), map[types.Type]bool{}) {
				validationErr = c.errorf(id, "unsupported type %s", v.Type())
			}
			if v, ok := c.info.Defs[id].(*types.Const); ok && !supportedConstantType(v.Type()) {
				validationErr = c.errorf(id, "unsupported constant type %s (use int, bool, or string)", v.Type())
			}
		}
		if expr, ok := n.(ast.Expr); ok {
			t := c.info.TypeOf(expr)
			_, list := c.listElement(t)
			_, _, dictionary := c.mapTypes(t)
			if (list || dictionary) && !c.supportedType(t, map[types.Type]bool{}) {
				validationErr = c.errorf(expr, "unsupported type %s", t)
			}
		}
		return true
	})
	if validationErr != nil {
		return "", validationErr
	}
	if err := c.lowerRanges(file); err != nil {
		return "", err
	}
	var functions []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.CONST {
				continue // go/types resolves constants, including grouped iota declarations.
			}
			if d.Tok != token.TYPE {
				return "", c.errorf(d, "top-level variables are not supported yet")
			}
			for _, spec := range d.Specs {
				t := spec.(*ast.TypeSpec)
				st, ok := t.Type.(*ast.StructType)
				if !ok || t.Assign.IsValid() || t.TypeParams != nil {
					return "", c.errorf(t, "only non-generic struct type declarations are supported")
				}
				for _, field := range st.Fields.List {
					if len(field.Names) == 0 || field.Tag != nil {
						return "", c.errorf(field, "embedded fields and struct tags are not supported")
					}
					for _, name := range field.Names {
						if name.Name == "_" {
							return "", c.errorf(name, "blank struct fields are not supported")
						}
					}
				}
			}
		case *ast.FuncDecl:
			c.optimizing, c.rooted, c.values = false, false, nil
			// Both backends accept the same language. The original lowering also
			// validates syntax before the optimized control-flow builder runs.
			fn, err := c.function(d)
			if err != nil {
				return "", err
			}
			if !options.DisableOptimizations {
				fn, err = c.optimizedFunction(d)
				if err != nil {
					return "", err
				}
			}
			functions = append(functions, fn)
		default:
			return "", c.errorf(decl, "unsupported declaration")
		}
	}
	var exports, wrappers []string
	if main != nil {
		exports = append(exports, "main/0")
		wrappers = append(wrappers, "main() -> try "+functionName("main")+"() after linglang_rt:finish_process() end.")
	}
	if tests != nil {
		exports = append(exports, "run_test/1")
		var clauses []string
		for _, test := range *tests {
			clauses = append(clauses, "run_test("+binaryString(test.Name)+") -> try "+functionName(test.Name)+"() after linglang_rt:finish_process() end")
		}
		clauses = append(clauses, "run_test(_) -> erlang:error(linglang_unknown_test)")
		wrappers = append(wrappers, strings.Join(clauses, ";\n")+".")
	}
	return "%% Generated by linglang.\n-module(linglang_program).\n-export([" + strings.Join(exports, ", ") + "]).\n\n" + strings.Join(wrappers, "\n\n") + "\n\n" + strings.Join(functions, "\n\n") + "\n", nil
}

func (c *compiler) supportedType(t types.Type, seen map[types.Type]bool) bool {
	if c.opaqueProcessType(t) {
		return true
	}
	if seen[t] {
		return true
	}
	seen[t] = true
	if element, ok := c.listElement(t); ok {
		return c.supportedType(element, seen)
	}
	if key, value, ok := c.mapTypes(t); ok {
		return supportedMapKey(key) && c.supportedType(value, seen)
	}
	switch t := t.(type) {
	case *types.Basic:
		return t.Kind() == types.Int || t.Kind() == types.Bool || t.Kind() == types.String
	case *types.Pointer:
		return c.supportedType(t.Elem(), seen)
	case *types.Named:
		_, ok := t.Underlying().(*types.Struct)
		return ok && c.supportedType(t.Underlying(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if t.Field(i).Embedded() || t.Field(i).Name() == "_" || t.Tag(i) != "" || !c.supportedType(t.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *compiler) errorf(n ast.Node, format string, args ...any) error {
	return fmt.Errorf("%s: %s", c.fset.Position(n.Pos()), fmt.Sprintf(format, args...))
}

func (c *compiler) fresh() string {
	c.next++
	return fmt.Sprintf("_V%d", c.next)
}

func (c *compiler) cell(obj types.Object) string {
	if name, ok := c.cells[obj]; ok {
		return name
	}
	name := c.fresh()
	c.cells[obj] = name
	return name
}

func functionName(name string) string { return "f_" + hex.EncodeToString([]byte(name)) }
func fieldName(name string) string    { return "field_" + hex.EncodeToString([]byte(name)) }

func (c *compiler) function(fn *ast.FuncDecl) (string, error) {
	if fn.Name.Name == "init" || fn.Name.Name == "_" {
		return "", c.errorf(fn, "init and blank function declarations are not supported")
	}
	if fn.Recv != nil || fn.Type.TypeParams != nil || fn.Body == nil {
		return "", c.errorf(fn, "methods, generics, and bodyless functions are not supported")
	}
	sig := c.info.Defs[fn.Name].Type().(*types.Signature)
	if sig.Variadic() || sig.Results().Len() > 1 {
		return "", c.errorf(fn, "variadic functions and multiple return values are not supported")
	}
	for i := 0; i < sig.Results().Len(); i++ {
		v := sig.Results().At(i)
		if v.Name() != "" || !c.supportedType(v.Type(), map[types.Type]bool{}) {
			return "", c.errorf(fn, "return type must be supported and unnamed")
		}
	}
	c.ret = c.fresh()
	c.loops = nil
	c.breaks = nil
	var args, setup []string
	for i := 0; i < sig.Params().Len(); i++ {
		param := sig.Params().At(i)
		arg := c.fresh()
		args = append(args, arg)
		setup = append(setup, c.cell(param)+" = linglang_rt:new("+arg+")")
	}
	body, err := c.block(fn.Body)
	if err != nil {
		return "", err
	}
	setup = append(setup, c.ret+" = make_ref()")
	result := c.fresh()
	setup = append(setup, "try "+body+" catch throw:{linglang_return, "+c.ret+", "+result+"} -> "+result+" end")
	return functionName(fn.Name.Name) + "(" + strings.Join(args, ", ") + ") ->\n    " + scoped(strings.Join(setup, ",\n    ")) + ".", nil
}

func (c *compiler) block(block *ast.BlockStmt) (string, error) {
	parts := []string{}
	for _, stmt := range block.List {
		part, err := c.statement(stmt)
		if err != nil {
			return "", err
		}
		parts = append(parts, "linglang_rt:safepoint()", part)
	}
	parts = append(parts, "ok")
	// A real scope avoids Erlang's unsafe-variable rules for branch-local bindings.
	return scoped(strings.Join(parts, ",\n        ")), nil
}

// Every expression statement has a temporary root frame. Declarations allocate
// their variable in the enclosing lexical frame after evaluating the initializer
// in its own temporary frame. There is no safe point during the value handoff.
func (c *compiler) statement(stmt ast.Stmt) (string, error) {
	code, err := c.statementCode(stmt)
	if err != nil {
		return "", err
	}
	switch s := stmt.(type) {
	case *ast.DeclStmt, *ast.BlockStmt, *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt:
		return code, nil
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			return code, nil
		}
	}
	return scoped(code), nil
}

func scoped(code string) string {
	return "linglang_rt:scope(fun() -> " + code + " end)"
}

func (c *compiler) statementCode(stmt ast.Stmt) (string, error) {
	switch s := stmt.(type) {
	case *ast.EmptyStmt:
		return "ok", nil
	case *ast.BlockStmt:
		return c.block(s)
	case *ast.ExprStmt:
		return c.expression(s.X)
	case *ast.DeclStmt:
		d := s.Decl.(*ast.GenDecl)
		if d.Tok == token.CONST {
			return "ok", nil
		}
		if d.Tok != token.VAR {
			return "", c.errorf(d, "only var declarations are supported inside functions")
		}
		var parts []string
		for _, spec := range d.Specs {
			v := spec.(*ast.ValueSpec)
			if len(v.Names) != 1 || len(v.Values) > 1 || v.Names[0].Name == "_" {
				return "", c.errorf(v, "declare one named variable at a time")
			}
			obj := c.info.Defs[v.Names[0]]
			value := c.zero(obj.Type())
			if len(v.Values) == 1 {
				var err error
				value, err = c.expression(v.Values[0])
				if err != nil {
					return "", err
				}
			}
			parts = append(parts, c.cell(obj)+" = linglang_rt:new("+scoped(value)+")")
		}
		return "begin " + strings.Join(parts, ", ") + " end", nil
	case *ast.AssignStmt:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			return "", c.errorf(s, "multiple assignment is not supported yet")
		}
		value, err := c.expression(s.Rhs[0])
		if err != nil {
			return "", err
		}
		if id, ok := s.Lhs[0].(*ast.Ident); ok && id.Name == "_" {
			if s.Tok != token.ASSIGN {
				return "", c.errorf(s, "unsupported blank assignment")
			}
			return value, nil
		}
		if s.Tok == token.DEFINE {
			id := s.Lhs[0].(*ast.Ident)
			return c.cell(c.info.Defs[id]) + " = linglang_rt:new(" + scoped(value) + ")", nil
		}
		address, err := c.address(s.Lhs[0])
		if err != nil {
			return "", err
		}
		if s.Tok == token.ASSIGN {
			return c.ordered([]string{address, value}, func(v []string) string {
				return "linglang_rt:write(" + v[0] + ", " + v[1] + ")"
			}), nil
		}
		op, ok := map[token.Token]string{token.ADD_ASSIGN: "add", token.SUB_ASSIGN: "sub", token.MUL_ASSIGN: "mul", token.QUO_ASSIGN: "divide", token.REM_ASSIGN: "remain"}[s.Tok]
		if !ok {
			return "", c.errorf(s, "unsupported assignment operator %s", s.Tok)
		}
		return c.ordered([]string{address}, func(v []string) string {
			return "linglang_rt:write(" + v[0] + ", " + c.ordered([]string{"linglang_rt:read(" + v[0] + ")", value}, func(x []string) string {
				return "linglang_rt:binary(" + op + ", " + x[0] + ", " + x[1] + ")"
			}) + ")"
		}), nil
	case *ast.IncDecStmt:
		address, err := c.address(s.X)
		if err != nil {
			return "", err
		}
		op := "add"
		if s.Tok == token.DEC {
			op = "sub"
		}
		return c.ordered([]string{address}, func(v []string) string {
			return "linglang_rt:write(" + v[0] + ", linglang_rt:binary(" + op + ", linglang_rt:read(" + v[0] + "), 1))"
		}), nil
	case *ast.ReturnStmt:
		value := "ok"
		if len(s.Results) == 1 {
			var err error
			value, err = c.expression(s.Results[0])
			if err != nil {
				return "", err
			}
		}
		return "throw({linglang_return, " + c.ret + ", " + value + "})", nil
	case *ast.IfStmt:
		if s.Init != nil {
			return "", c.errorf(s, "if initializers are not supported yet")
		}
		cond, err := c.expression(s.Cond)
		if err != nil {
			return "", err
		}
		body, err := c.block(s.Body)
		if err != nil {
			return "", err
		}
		other := "ok"
		if s.Else != nil {
			other, err = c.statement(s.Else)
			if err != nil {
				return "", err
			}
		}
		return "case " + scoped(cond) + " of true -> " + body + "; false -> " + other + " end", nil
	case *ast.SwitchStmt:
		return c.switchStatement(s)
	case *ast.ForStmt:
		init, cond, post := "ok", "true", "ok"
		var err error
		if s.Init != nil {
			init, err = c.statement(s.Init)
			if err != nil {
				return "", err
			}
		}
		// Go-style loop declarations have a distinct variable each iteration.
		// Compile callbacks against a parameter cell instead of the initial cell.
		iteration, initial := "", ""
		if assign, ok := s.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
			obj := c.info.Defs[assign.Lhs[0].(*ast.Ident)]
			initial = c.cell(obj)
			iteration = c.fresh()
			c.cells[obj] = iteration
			defer func() { c.cells[obj] = initial }()
		}
		if s.Cond != nil {
			cond, err = c.expression(s.Cond)
			if err != nil {
				return "", err
			}
		}
		if s.Post != nil {
			post, err = c.statement(s.Post)
			if err != nil {
				return "", err
			}
		}
		loop := c.fresh()
		c.loops = append(c.loops, loop)
		c.breaks = append(c.breaks, loop)
		body, err := c.block(s.Body)
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if err != nil {
			return "", err
		}
		if iteration != "" {
			return scoped(init + ", " + loop + " = make_ref(), linglang_rt:loop_cell(fun(" + iteration + ") -> " + cond + " end, fun(" + iteration + ") -> " + body + " end, fun(" + iteration + ") -> " + post + " end, " + loop + ", " + initial + ")"), nil
		}
		// Wrap loop initialization so its variables cannot escape into the next loop.
		return scoped(init + ", " + loop + " = make_ref(), linglang_rt:loop(fun() -> " + cond + " end, fun() -> " + body + " end, fun() -> " + post + " end, " + loop + ")"), nil
	case *ast.BranchStmt:
		if s.Label != nil || (s.Tok != token.BREAK && s.Tok != token.CONTINUE) {
			return "", c.errorf(s, "only unlabelled break and continue are supported; fallthrough is not supported")
		}
		targets := c.loops
		if s.Tok == token.BREAK {
			targets = c.breaks
		}
		if len(targets) == 0 {
			return "", c.errorf(s, "%s requires an enclosing loop or switch", s.Tok)
		}
		return "throw({linglang_" + s.Tok.String() + ", " + targets[len(targets)-1] + "})", nil
	default:
		return "", c.errorf(stmt, "unsupported statement %T", stmt)
	}
}

func (c *compiler) ordered(expressions []string, finish func([]string) string) string {
	var parts, vars []string
	for _, expr := range expressions {
		v := c.fresh()
		vars = append(vars, v)
		if c.optimizing && !c.rooted {
			parts = append(parts, v+" = "+expr)
		} else {
			parts = append(parts, v+" = linglang_rt:keep("+expr+")")
		}
	}
	parts = append(parts, finish(vars))
	return "(begin " + strings.Join(parts, ", ") + " end)"
}

func (c *compiler) expression(expr ast.Expr) (string, error) {
	if operation := c.rangeOps[expr]; operation != "" {
		call := expr.(*ast.CallExpr)
		value, err := c.expression(call.Args[0])
		return "linglang_rt:list_" + operation + "(" + value + ")", err
	}
	if value := c.info.Types[expr].Value; value != nil {
		switch value.Kind() {
		case constant.Int:
			return value.ExactString(), nil
		case constant.Bool:
			return strconv.FormatBool(constant.BoolVal(value)), nil
		case constant.String:
			return binaryString(constant.StringVal(value)), nil
		default:
			return "", c.errorf(expr, "only integer, boolean, and string constants are supported")
		}
	}
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return c.expression(e.X)
	case *ast.Ident:
		obj := c.info.Uses[e]
		if _, ok := obj.(*types.Nil); ok {
			return "nil", nil
		}
		if _, ok := obj.(*types.Var); !ok {
			return "", c.errorf(e, "function values are not supported")
		}
		if value, ok := c.values[obj]; ok {
			return value, nil
		}
		return "linglang_rt:read(" + c.cell(obj) + ")", nil
	case *ast.StarExpr:
		ptr, err := c.expression(e.X)
		return "linglang_rt:read(" + ptr + ")", err
	case *ast.SelectorExpr:
		base, err := c.expression(e.X)
		return "linglang_rt:field_value(" + base + ", " + fieldName(e.Sel.Name) + ")", err
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return c.address(e.X)
		}
		x, err := c.expression(e.X)
		if err != nil {
			return "", err
		}
		switch e.Op {
		case token.NOT:
			return "(not (" + x + "))", nil
		case token.SUB:
			return "linglang_rt:binary(sub, 0, " + x + ")", nil
		case token.ADD:
			return x, nil
		default:
			return "", c.errorf(e, "unsupported unary operator %s", e.Op)
		}
	case *ast.BinaryExpr:
		left, err := c.expression(e.X)
		if err != nil {
			return "", err
		}
		right, err := c.expression(e.Y)
		if err != nil {
			return "", err
		}
		if e.Op == token.LAND {
			return "((" + left + ") andalso (" + right + "))", nil
		}
		if e.Op == token.LOR {
			return "((" + left + ") orelse (" + right + "))", nil
		}
		op, ok := map[token.Token]string{token.ADD: "add", token.SUB: "sub", token.MUL: "mul", token.QUO: "divide", token.REM: "remain", token.EQL: "eq", token.NEQ: "ne", token.LSS: "lt", token.LEQ: "le", token.GTR: "gt", token.GEQ: "ge"}[e.Op]
		if !ok {
			return "", c.errorf(e, "unsupported binary operator %s", e.Op)
		}
		return c.ordered([]string{left, right}, func(v []string) string { return "linglang_rt:binary(" + op + ", " + v[0] + ", " + v[1] + ")" }), nil
	case *ast.CompositeLit:
		if _, ok := c.listElement(c.info.TypeOf(e)); ok {
			return c.listLiteral(e)
		}
		if _, _, ok := c.mapTypes(c.info.TypeOf(e)); ok {
			return c.mapLiteral(e)
		}
		_, ok := c.info.TypeOf(e).Underlying().(*types.Struct)
		if !ok {
			return "", c.errorf(e, "only struct literals are supported")
		}
		var names, values []string
		for _, element := range e.Elts {
			kv, ok := element.(*ast.KeyValueExpr)
			if !ok {
				return "", c.errorf(element, "struct literals require named fields")
			}
			name := kv.Key.(*ast.Ident).Name
			value, err := c.expression(kv.Value)
			if err != nil {
				return "", err
			}
			names = append(names, fieldName(name))
			values = append(values, value)
		}
		return c.ordered(values, func(v []string) string {
			if len(v) == 0 {
				return c.zero(c.info.TypeOf(e))
			}
			var fields []string
			for i := range v {
				fields = append(fields, names[i]+" := "+v[i])
			}
			return "(" + c.zero(c.info.TypeOf(e)) + ")#{" + strings.Join(fields, ", ") + "}"
		}), nil
	case *ast.CallExpr:
		if code, handled, err := c.mapCall(e); handled {
			return code, err
		}
		if code, handled, err := c.listBuiltin(e); handled {
			return code, err
		}
		if code, handled, err := c.standardCall(e); handled {
			return code, err
		}
		if code, intrinsic, err := c.processCall(e); intrinsic {
			return code, err
		}
		id, ok := e.Fun.(*ast.Ident)
		if !ok || e.Ellipsis.IsValid() {
			return "", c.errorf(e, "only direct function calls are supported")
		}
		obj := c.info.Uses[id]
		_, builtin := obj.(*types.Builtin)
		if builtin && id.Name == "panic" {
			if basic, ok := c.info.TypeOf(e.Args[0]).(*types.Basic); !ok || (basic.Kind() != types.String && basic.Kind() != types.UntypedString) {
				return "", c.errorf(e, "panic requires a string reason")
			}
			value, err := c.expression(e.Args[0])
			return "erlang:error({linglang_panic, " + value + "})", err
		}
		if builtin && id.Name != "print" && id.Name != "println" {
			return "", c.errorf(e, "unsupported builtin %s", id.Name)
		}
		if _, ok := obj.(*types.Func); !ok && !builtin {
			return "", c.errorf(e, "type conversions and function values are not supported")
		}
		var args []string
		for _, arg := range e.Args {
			value, err := c.expression(arg)
			if err != nil {
				return "", err
			}
			args = append(args, value)
		}
		return c.ordered(args, func(v []string) string {
			if builtin {
				return "linglang_rt:print([" + strings.Join(v, ", ") + "], " + strconv.FormatBool(id.Name == "println") + ")"
			}
			return functionName(id.Name) + "(" + strings.Join(v, ", ") + ")"
		}), nil
	default:
		return "", c.errorf(expr, "unsupported expression %T", expr)
	}
}

func (c *compiler) address(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return c.address(e.X)
	case *ast.Ident:
		if _, ok := c.values[c.info.Uses[e]]; ok {
			return "", c.errorf(e, "internal error: address requested for a value local")
		}
		return c.cell(c.info.Uses[e]), nil
	case *ast.StarExpr:
		ptr, err := c.expression(e.X)
		return "linglang_rt:deref(" + ptr + ")", err
	case *ast.SelectorExpr:
		var base string
		var err error
		if _, pointer := c.info.TypeOf(e.X).(*types.Pointer); pointer {
			base, err = c.expression(e.X)
		} else {
			base, err = c.address(e.X)
		}
		return "linglang_rt:field(" + base + ", " + fieldName(e.Sel.Name) + ")", err
	case *ast.CompositeLit:
		value, err := c.expression(e)
		return "linglang_rt:new(" + value + ")", err
	default:
		return "", c.errorf(expr, "unsupported reference target")
	}
}

func (c *compiler) zero(t types.Type) string {
	if _, _, ok := c.mapTypes(t); ok {
		return "nil"
	}
	if _, ok := c.listElement(t); ok {
		return "nil"
	}
	if c.opaqueProcessType(t) {
		return "nil"
	}
	switch t := t.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return "false"
		case types.String:
			return "<<>>"
		default:
			return "0"
		}
	case *types.Pointer:
		return "nil"
	case *types.Struct:
		var fields []string
		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			fields = append(fields, fieldName(field.Name())+" => "+c.zero(field.Type()))
		}
		return "#{" + strings.Join(fields, ", ") + "}"
	default:
		panic("zero called for unsupported type: " + t.String())
	}
}

func binaryString(s string) string {
	parts := make([]string, len(s))
	for i, b := range []byte(s) {
		parts[i] = strconv.Itoa(int(b))
	}
	return "<<" + strings.Join(parts, ",") + ">>"
}
