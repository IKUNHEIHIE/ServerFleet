//go:build ignore

// Run with go run scripts/api-inventory.go. This is a source declaration index,
// not proof that an optional handler is enabled in a particular deployment.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type declaration struct {
	Source string `json:"source"`
	Owner  string `json:"owner"`
}
type route struct {
	Method       string        `json:"method"`
	Path         string        `json:"path"`
	Declarations []declaration `json:"declarations"`
}

func main() {
	pattern := regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) (/[^ ]*)$`)
	entries := map[string]*route{}
	add := func(value ast.Expr, source, owner string) {
		literal, ok := value.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return
		}
		s, err := strconv.Unquote(literal.Value)
		if err != nil {
			return
		}
		m := pattern.FindStringSubmatch(s)
		if m == nil {
			return
		}
		r := entries[s]
		if r == nil {
			r = &route{Method: m[1], Path: m[2]}
			entries[s] = r
		}
		d := declaration{source, owner}
		for _, old := range r.Declarations {
			if old == d {
				return
			}
		}
		r.Declarations = append(r.Declarations, d)
	}
	for _, root := range []string{"apps/controller/server", "internal"} {
		err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				owner := fn.Name.Name
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.CallExpr:
						if sel, ok := n.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Handle" || sel.Sel.Name == "HandleFunc") && len(n.Args) > 0 {
							add(n.Args[0], filepath.ToSlash(name), owner)
						}
					case *ast.RangeStmt:
						if values, ok := n.X.(*ast.CompositeLit); ok {
							for _, value := range values.Elts {
								add(value, filepath.ToSlash(name), owner)
							}
						}
						// Enrollment aliases use "POST " + path in a finite
						// literal range. Resolve only that auditable form.
						values, literalRange := n.X.(*ast.CompositeLit)
						variable, named := n.Value.(*ast.Ident)
						if literalRange && named {
							ast.Inspect(n.Body, func(child ast.Node) bool {
								call, ok := child.(*ast.CallExpr)
								if !ok || len(call.Args) == 0 {
									return true
								}
								selector, ok := call.Fun.(*ast.SelectorExpr)
								if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
									return true
								}
								concat, ok := call.Args[0].(*ast.BinaryExpr)
								if !ok || concat.Op != token.ADD {
									return true
								}
								prefix, ok := concat.X.(*ast.BasicLit)
								if !ok || prefix.Kind != token.STRING {
									return true
								}
								suffix, ok := concat.Y.(*ast.Ident)
								if !ok || suffix.Name != variable.Name {
									return true
								}
								method, err := strconv.Unquote(prefix.Value)
								if err != nil {
									return true
								}
								for _, value := range values.Elts {
									part, ok := value.(*ast.BasicLit)
									if !ok || part.Kind != token.STRING {
										continue
									}
									path, err := strconv.Unquote(part.Value)
									if err != nil {
										continue
									}
									add(&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(method + path)}, filepath.ToSlash(name), owner)
								}
								return true
							})
						}
					case *ast.ReturnStmt:
						if strings.HasSuffix(owner, "Patterns") {
							for _, result := range n.Results {
								if values, ok := result.(*ast.CompositeLit); ok {
									for _, value := range values.Elts {
										add(value, filepath.ToSlash(name), owner)
									}
								}
							}
						}
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]route, 0, len(keys))
	for _, key := range keys {
		r := entries[key]
		sort.Slice(r.Declarations, func(i, j int) bool {
			a, b := r.Declarations[i], r.Declarations[j]
			return a.Source+":"+a.Owner < b.Source+":"+b.Owner
		})
		out = append(out, *r)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
