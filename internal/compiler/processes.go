package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"strings"
)

// These declarations exist only for Go's type checker. Identity, not spelling,
// determines intrinsic calls. No prelude body is emitted as language code.
const processPrelude = `package main
type Pid chan int
type Monitor chan bool
type Supervisor chan string
type Process struct { pid Pid; monitor Monitor }
type ChildOptions struct { restart string; shutdownMillis int }
type Child struct { pid Pid; ok bool; reason string }
type Delivery[T any] struct { value T; ok bool }
type Exit struct { pid Pid; ok bool; normal bool; reason string }
type List[T any] []T
func prepend[T any](value T, items List[T]) List[T] { return nil }
func head[T any](items List[T]) Delivery[T] { return Delivery[T]{} }
func tail[T any](items List[T]) List[T] { return nil }
func self() Pid { return nil }
func spawn[T any](worker func(T), args T) Pid { return nil }
func spawnMonitor[T any](worker func(T), args T) Process { return Process{} }
func send[T any](pid Pid, value T) {}
func receive[T any](timeout int) Delivery[T] { return Delivery[T]{} }
func monitor(pid Pid) Monitor { return nil }
func wait(watcher Monitor, timeout int) Exit { return Exit{} }
func demonitor(watcher Monitor) bool { return false }
func startSupervisor(maxRestarts int, withinSeconds int) Supervisor { return nil }
func supervisorPid(supervisor Supervisor) Pid { return nil }
func supervise[T any](supervisor Supervisor, name string, worker func(T), args T, options ChildOptions) Child { return Child{} }
func lookupChild(supervisor Supervisor, name string) Child { return Child{} }
func restartChild(supervisor Supervisor, name string) Child { return Child{} }
func stopChild(supervisor Supervisor, name string) bool { return false }
func removeChild(supervisor Supervisor, name string) bool { return false }
func stopSupervisor(supervisor Supervisor) bool { return false }
`

func (c *compiler) prelude() (*ast.File, error) {
	return parser.ParseFile(c.fset, "<linglang-prelude>", processPrelude+standardPrelude+mapPrelude, 0)
}

func (c *compiler) registerIntrinsics(file *ast.File) {
	c.intrinsics = map[types.Object]string{}
	c.processTypes = map[string]*types.Named{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			c.intrinsics[c.info.Defs[d.Name]] = d.Name.Name
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				t := spec.(*ast.TypeSpec)
				c.processTypes[t.Name.Name] = c.info.Defs[t.Name].Type().(*types.Named)
			}
		}
	}
}

func callIdentifier(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.IndexExpr:
		return callIdentifier(e.X)
	case *ast.IndexListExpr:
		return callIdentifier(e.X)
	case *ast.ParenExpr:
		return callIdentifier(e.X)
	default:
		return nil
	}
}

func (c *compiler) processType(t types.Type, name string) bool {
	n, ok := t.(*types.Named)
	return ok && n.Origin() == c.processTypes[name]
}

func (c *compiler) opaqueProcessType(t types.Type) bool {
	return c.processType(t, "Pid") || c.processType(t, "Monitor") || c.processType(t, "Supervisor")
}

// The schema is both the selective-receive tag and a runtime validator. Named
// identity and complete field shapes prevent structurally similar messages, or
// mismatched versions of a message, from being silently confused.
func (c *compiler) messageSchema(t types.Type) (string, error) {
	return c.messageSchemaSeen(t, map[types.Type]bool{})
}

func (c *compiler) messageSchemaSeen(t types.Type, seen map[types.Type]bool) (string, error) {
	if seen[t] {
		return "", fmt.Errorf("recursive message types are not supported")
	}
	seen[t] = true
	defer delete(seen, t)
	if key, value, ok := c.mapTypes(t); ok {
		if !supportedMapKey(key) {
			return "", fmt.Errorf("Map keys must be int or string")
		}
		keyShape, err := c.messageSchemaSeen(key, seen)
		if err != nil {
			return "", err
		}
		valueShape, err := c.messageSchemaSeen(value, seen)
		if err != nil {
			return "", fmt.Errorf("map value: %w", err)
		}
		return "{map, " + keyShape + ", " + valueShape + "}", nil
	}
	if element, ok := c.listElement(t); ok {
		shape, err := c.messageSchemaSeen(element, seen)
		if err != nil {
			return "", fmt.Errorf("list element: %w", err)
		}
		return "{list, " + shape + "}", nil
	}
	if c.processType(t, "Pid") {
		return "pid", nil
	}
	if c.processType(t, "Monitor") {
		return "", fmt.Errorf("monitor handles belong to their creating process")
	}
	if c.processType(t, "Supervisor") {
		return "", fmt.Errorf("supervisor handles belong to their creating process; share supervisorPid instead")
	}
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Int:
			return "int", nil
		case types.Bool:
			return "bool", nil
		case types.String:
			return "string", nil
		}
	case *types.Pointer:
		return "", fmt.Errorf("process-local pointers cannot cross a process boundary")
	case *types.Named:
		shape, err := c.messageSchemaSeen(t.Underlying(), seen)
		if err != nil {
			return "", err
		}
		name := types.TypeString(t, func(pkg *types.Package) string { return pkg.Path() })
		return "{named, " + binaryString(name) + ", " + shape + "}", nil
	case *types.Struct:
		var fields []string
		for i := 0; i < t.NumFields(); i++ {
			field := t.Field(i)
			shape, err := c.messageSchemaSeen(field.Type(), seen)
			if err != nil {
				return "", fmt.Errorf("field %s: %w", field.Name(), err)
			}
			fields = append(fields, "{"+fieldName(field.Name())+", "+shape+"}")
		}
		return "{struct, [" + strings.Join(fields, ", ") + "]}", nil
	}
	return "", fmt.Errorf("unsupported message type %s", t)
}

func (c *compiler) processCall(call *ast.CallExpr) (string, bool, error) {
	id := callIdentifier(call.Fun)
	if id == nil {
		return "", false, nil
	}
	name, ok := c.intrinsics[c.info.Uses[id]]
	if !ok {
		return "", false, nil
	}
	if name == "prepend" || name == "head" || name == "tail" {
		code, err := c.listIntrinsic(call, name)
		return code, true, err
	}
	sig := c.info.TypeOf(call.Fun).(*types.Signature)
	var schema string
	var err error
	switch name {
	case "spawn", "spawnMonitor", "send":
		schema, err = c.messageSchema(sig.Params().At(1).Type())
	case "receive":
		delivery := sig.Results().At(0).Type().(*types.Named)
		schema, err = c.messageSchema(delivery.TypeArgs().At(0))
	case "supervise":
		schema, err = c.messageSchema(sig.Params().At(3).Type())
	}
	if err != nil {
		return "", true, c.errorf(call, "%s: %v", name, err)
	}
	if name == "spawn" || name == "spawnMonitor" || name == "supervise" {
		workerIndex := 0
		if name == "supervise" {
			workerIndex = 2
		}
		worker, ok := call.Args[workerIndex].(*ast.Ident)
		if !ok {
			return "", true, c.errorf(call.Args[workerIndex], "%s requires a named language function, not a closure", name)
		}
		obj, ok := c.info.Uses[worker].(*types.Func)
		if !ok || c.intrinsics[obj] != "" {
			return "", true, c.errorf(worker, "%s requires a named language function", name)
		}
		if name == "supervise" {
			var args []string
			for i, arg := range call.Args {
				if i == workerIndex {
					continue
				}
				value, err := c.expression(arg)
				if err != nil {
					return "", true, err
				}
				args = append(args, value)
			}
			return c.ordered(args, func(v []string) string {
				return "linglang_sup:add(" + v[0] + ", " + v[1] + ", fun " + functionName(worker.Name) + "/1, " + v[2] + ", " + schema + ", " + v[3] + ")"
			}), true, nil
		}
		arg, err := c.expression(call.Args[1])
		if err != nil {
			return "", true, err
		}
		monitored := "false"
		if name == "spawnMonitor" {
			monitored = "true"
		}
		return c.ordered([]string{arg}, func(v []string) string {
			return "linglang_rt:spawn_process(fun " + functionName(worker.Name) + "/1, " + v[0] + ", " + schema + ", " + monitored + ")"
		}), true, nil
	}
	var args []string
	for _, arg := range call.Args {
		value, err := c.expression(arg)
		if err != nil {
			return "", true, err
		}
		args = append(args, value)
	}
	return c.ordered(args, func(v []string) string {
		switch name {
		case "self":
			return "self()"
		case "send":
			return "linglang_rt:send_message(" + v[0] + ", " + schema + ", " + v[1] + ")"
		case "receive":
			t := sig.Results().At(0).Type().(*types.Named).TypeArgs().At(0)
			return "linglang_rt:receive_message(" + schema + ", " + v[0] + ", " + c.zero(t) + ")"
		case "monitor":
			return "linglang_rt:monitor_process(" + v[0] + ")"
		case "wait":
			return "linglang_rt:wait_process(" + v[0] + ", " + v[1] + ")"
		case "demonitor":
			return "linglang_rt:demonitor_process(" + v[0] + ")"
		case "startSupervisor":
			return "linglang_sup:start(" + v[0] + ", " + v[1] + ")"
		case "supervisorPid":
			return "linglang_sup:pid(" + v[0] + ")"
		case "lookupChild":
			return "linglang_sup:lookup(" + v[0] + ", " + v[1] + ")"
		case "restartChild":
			return "linglang_sup:restart(" + v[0] + ", " + v[1] + ")"
		case "stopChild":
			return "linglang_sup:stop_child(" + v[0] + ", " + v[1] + ")"
		case "removeChild":
			return "linglang_sup:remove_child(" + v[0] + ", " + v[1] + ")"
		case "stopSupervisor":
			return "linglang_sup:stop(" + v[0] + ")"
		default:
			panic("unknown process intrinsic")
		}
	}), true, nil
}

// Opaque handles may only be created by runtime operations (or zero initialized).
func (c *compiler) validateProcessSyntax(file *ast.File) error {
	var err error
	ast.Inspect(file, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if id := callIdentifier(call.Fun); id != nil && c.intrinsics[c.info.Uses[id]] != "" && call.Ellipsis.IsValid() {
				err = c.errorf(call, "variadic process calls are not supported")
			}
		}
		if literal, ok := n.(*ast.CompositeLit); ok && c.opaqueProcessType(c.info.TypeOf(literal)) {
			err = c.errorf(literal, "process handles are opaque; use process operations to create them")
		}
		return true
	})
	return err
}
