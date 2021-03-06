package d

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/kr/pretty"
)

var colorizeEnabled = true

// argName returns the source text of the given argument if it's a variable or
// an expression. If the argument is something else, like a literal, argName
// returns an empty string.
func argName(arg ast.Expr) string {
	name := ""
	switch a := arg.(type) {
	case *ast.Ident:
		if a.Obj.Kind == ast.Var || a.Obj.Kind == ast.Con {
			name = a.Obj.Name
		}
	case *ast.BinaryExpr,
		*ast.CallExpr,
		*ast.IndexExpr,
		*ast.KeyValueExpr,
		*ast.ParenExpr,
		*ast.SelectorExpr,
		*ast.SliceExpr,
		*ast.TypeAssertExpr,
		*ast.UnaryExpr:
		name = exprToString(arg)
	}
	return name
}

// argNames finds the d.D() call at the given filename/line number and
// returns its arguments as a slice of strings. If the argument is a literal,
// argNames will return an empty string at the index position of that argument.
// For example, d.D(ip, port, 5432) would return []string{"ip", "port", ""}.
// argNames returns an error if the source text cannot be parsed.
func argNames(filename string, line int) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %q: %v", filename, err)
	}

	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, is := n.(*ast.CallExpr)
		if !is {
			// The node is not a function call.
			return true // visit next node
		}

		if fset.Position(call.End()).Line != line {
			// The node is a function call, but it's on the wrong line.
			return true
		}

		if !isDCall(call) {
			// The node is a function call on correct line, but it's not a Q()
			// function.
			return true
		}

		for _, arg := range call.Args {
			names = append(names, argName(arg))
		}
		return true
	})

	return names, nil
}

// argWidth returns the number of characters that will be seen when the given
// argument is printed at the terminal.
func argWidth(arg string) int {
	// Strip zero-width characters.
	replacer := strings.NewReplacer(
		"\n", "",
		"\t", "",
		"\r", "",
		"\f", "",
		"\v", "",
		string(bold), "",
		string(yellow), "",
		string(cyan), "",
		string(endColor), "",
	)
	s := replacer.Replace(arg)
	return utf8.RuneCountInString(s)
}

// colorize returns the given text encapsulated in ANSI escape codes that
// give the text color in the terminal.
func colorize(text string, c color) string {
	if !colorizeEnabled {
		return text
	}
	return string(c) + text + string(endColor)
}

// exprToString returns the source text underlying the given ast.Expr.
func exprToString(arg ast.Expr) string {
	var buf bytes.Buffer
	fset := token.NewFileSet()
	printer.Fprint(&buf, fset, arg)

	// CallExpr will be multi-line and indented with tabs. replace tabs with
	// spaces so we can better control formatting during output().
	return strings.Replace(buf.String(), "\t", "    ", -1)
}

// formatArgs converts the given args to pretty-printed, colorized strings.
func formatArgs(args ...interface{}) []string {
	formatted := make([]string, 0, len(args))
	for _, a := range args {
		s := colorize(pretty.Sprint(a), cyan)
		formatted = append(formatted, s)
	}
	return formatted
}

// getCallerInfo returns the name, file, and line number of the function calling
// d.D().
func getCallerInfo() (funcName, file string, line int, err error) {
	const callDepth = 2 // user code calls d.D() which calls std.log().
	pc, file, line, ok := runtime.Caller(callDepth)
	if !ok {
		return "", "", 0, errors.New("failed to get info about the function calling d.D")
	}

	funcName = runtime.FuncForPC(pc).Name()
	return funcName, file, line, nil
}

// prependArgName turns argument names and values into name=value strings, e.g.
// "port=443", "3+2=5". If the name is given, it will be bolded using ANSI
// color codes. If no name is given, just the value will be returned.
func prependArgName(names, values []string) []string {
	prepended := make([]string, len(values))
	for i, name := range names {
		if name == "" {
			prepended[i] = values[i]
			continue
		}
		name = colorize(name, bold)
		prepended[i] = fmt.Sprintf("%s=%s", name, values[i])
	}
	return prepended
}

// isDCall returns true if the given function call expression is D() or d.D().
func isDCall(n *ast.CallExpr) bool {
	return isDFunction(n) || isDPackage(n)
}

// isDFunction returns true if the given function call expression is D().
func isDFunction(n *ast.CallExpr) bool {
	ident, is := n.Fun.(*ast.Ident)
	if !is {
		return false
	}
	return ident.Name == "D"
}

// isDPackage returns true if the given function call expression is in the d
// package. Since D() is the only exported function from the d package, this is
// sufficient for determining that we've found D() in the source text.
func isDPackage(n *ast.CallExpr) bool {
	sel, is := n.Fun.(*ast.SelectorExpr) // SelectorExpr example: a.B()
	if !is {
		return false
	}
	ident, is := sel.X.(*ast.Ident) // sel.X is the part that precedes the .
	if !is {
		return false
	}
	return ident.Name == "d"
}
