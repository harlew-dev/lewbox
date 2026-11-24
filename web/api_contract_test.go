package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// These tests keep the hand-written console client in src/lib/api.ts honest
// against the Go API. They parse both sides rather than importing one from
// the other, because the console has no Go types and the API has no TS ones.

var (
	// A route registration in internal/httpapi/router.go:
	// mux.HandleFunc("GET "+Prefix+"/clusters/{cluster}", clusters.Show)
	handleFunc = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) "\s*\+\s*Prefix\s*\+\s*"([^"]+)"`)
	// The static docs handlers are registered with serveDocs, not HandleFunc.
	serveDocsCall = regexp.MustCompile(`serveDocs\(mux, Prefix\+"([^"]+)"`)
	// A statement in api.ts: return request<Type>('PATH', ...
	requestCall = regexp.MustCompile(`return request<[^>\n]*>\(`)
	// ${ref(value)} is one path segment.
	pathRef = regexp.MustCompile(`\$\{ref\([^}]*\)\}`)
	// {cluster} in the router is the same segment as ${ref(cluster)}.
	routeParam = regexp.MustCompile(`\{[^}]+\}`)
	// A literal /v0 path in a call whose path is an expression (the docs
	// book ternary).
	literalPath = regexp.MustCompile(`['"](/v0/[^'"]*)['"]`)
	// method: 'POST' in the call's options, or an expression such as
	// method: disabled ? 'POST' : 'DELETE'.
	methodOption  = regexp.MustCompile(`method:\s*([^,\n}]+)`)
	methodLiteral = regexp.MustCompile(`'([A-Z]+)'`)
)

type apiCall struct {
	method string
	path   string
	line   int
}

// TestAPIRoutesExist fails when api.ts calls a path and method that the
// router does not serve: a typo, a renamed route, or a route that was removed.
func TestAPIRoutesExist(t *testing.T) {
	registered := registeredRoutes(t)
	if len(registered) < 50 {
		t.Fatalf("parsed only %d registered routes; has router.go changed shape?", len(registered))
	}
	calls := apiCalls(t)
	if len(calls) < 50 {
		t.Fatalf("parsed only %d console API calls; has api.ts changed shape?", len(calls))
	}
	for _, call := range calls {
		if _, ok := registered[call.method+" "+call.path]; !ok {
			t.Errorf("api.ts:%d calls %s %s, but the router has no such route", call.line, call.method, call.path)
		}
	}
}

func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("../internal/httpapi/router.go")
	if err != nil {
		t.Fatal(err)
	}
	prefix := regexp.MustCompile(`const Prefix = "([^"]+)"`).FindStringSubmatch(string(src))
	if prefix == nil {
		t.Fatal("router.go has no Prefix constant")
	}
	routes := make(map[string]bool)
	for _, m := range handleFunc.FindAllStringSubmatch(string(src), -1) {
		routes[m[1]+" "+prefix[1]+routeParam.ReplaceAllString(m[2], "{}")] = true
	}
	// The two books are served by serveDocs under GET prefixes. api.ts reads
	// their nav and pages through the same request helper.
	for _, m := range serveDocsCall.FindAllStringSubmatch(string(src), -1) {
		routes["GET "+prefix[1]+m[1]] = true
	}
	return routes
}

// apiCalls returns every request the console makes, with a normalized path:
// path parameters are {}, and query strings are dropped.
func apiCalls(t *testing.T) []apiCall {
	t.Helper()
	src, err := os.ReadFile("src/lib/api.ts")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	calls := make([]apiCall, 0, 80)
	for _, loc := range requestCall.FindAllStringIndex(text, -1) {
		args, ok := callArguments(text, loc[1]-1)
		if !ok {
			t.Errorf("api.ts:%d: cannot find the end of this request call", lineAt(text, loc[0]))
			continue
		}
		line := lineAt(text, loc[0])
		methods := callMethods(args)
		paths := make([]string, 0, 2)
		if path, ok := quotedPath(args); ok {
			if normal, ok := normalizePath(path); ok {
				paths = append(paths, normal)
			}
		} else {
			// A call whose path is an expression still names its paths as
			// literals, as docsNav does with the book ternary.
			for _, m := range literalPath.FindAllStringSubmatch(args, -1) {
				if normal, ok := normalizePath(m[1]); ok {
					paths = append(paths, normal)
				}
			}
		}
		for _, method := range methods {
			for _, path := range paths {
				calls = append(calls, apiCall{method: method, path: path, line: line})
			}
		}
	}
	return calls
}

// callArguments returns the text inside the parentheses that start at open.
func callArguments(text string, open int) (string, bool) {
	depth := 0
	var quote byte
	for i := open; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[open+1 : i], true
			}
		}
	}
	return "", false
}

// callMethods returns every method the call can use. A conditional method
// names both branches, and the caller must serve both.
func callMethods(args string) []string {
	m := methodOption.FindStringSubmatch(args)
	if m == nil {
		return []string{"GET"}
	}
	var methods []string
	for _, lit := range methodLiteral.FindAllStringSubmatch(m[1], -1) {
		methods = append(methods, lit[1])
	}
	if len(methods) == 0 {
		return []string{"GET"}
	}
	return methods
}

// quotedPath returns the path when the call's first argument is a string.
func quotedPath(args string) (string, bool) {
	args = strings.TrimSpace(args)
	if args == "" || (args[0] != '\'' && args[0] != '"' && args[0] != '`') {
		return "", false
	}
	quote := args[0]
	for i := 1; i < len(args); i++ {
		if args[i] == '\\' {
			i++
			continue
		}
		if args[i] == quote {
			return args[1:i], true
		}
	}
	return "", false
}

// normalizePath turns a template into the router's shape: /v0/clusters/{}.
func normalizePath(path string) (string, bool) {
	if !strings.HasPrefix(path, "/v0/") {
		return "", false
	}
	path = pathRef.ReplaceAllString(path, "{}")
	if i := strings.IndexAny(path, "?"); i >= 0 {
		path = path[:i]
	}
	if i := strings.Index(path, "${"); i >= 0 {
		path = path[:i]
	}
	return path, strings.HasPrefix(path, "/v0/")
}

func lineAt(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

var (
	errorCodeLiteral = regexp.MustCompile(`\bcode\s*(?::|===|!==)\s*'([a-z_]+)'`)
	// typeof x.code === 'string' compares a type, not an error code.
	typeofResult = map[string]bool{
		"string": true, "number": true, "boolean": true, "object": true, "undefined": true,
	}
)

// TestErrorCodesExist fails when the console branches on, or fakes, an error
// code the API cannot return.
func TestErrorCodesExist(t *testing.T) {
	known := serviceErrorCodes(t)
	known["internal"] = true // httpapi's error for anything unexpected
	// Minted by api.ts itself, never by the server.
	known["unknown"] = true
	known["invalid_response"] = true
	if len(known) < 10 {
		t.Fatalf("parsed only %d known error codes; has errors.go changed shape?", len(known))
	}

	seen := make(map[string]bool)
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		for _, m := range errorCodeLiteral.FindAllStringSubmatchIndex(text, -1) {
			code := text[m[2]:m[3]]
			if typeofResult[code] {
				continue // typeof x.code === 'string', not an API code
			}
			seen[code] = true
			if !known[code] {
				t.Errorf("%s:%d uses error code %q, which is not in service/errors.go or an httpapi/client code",
					path, lineAt(text, m[0]), code)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 5 {
		t.Fatalf("found only %d error codes in the console; has the pattern stopped matching?", len(seen))
	}
}

func serviceErrorCodes(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("../internal/service/errors.go")
	if err != nil {
		t.Fatal(err)
	}
	codes := make(map[string]bool)
	for _, m := range regexp.MustCompile(`Code\w+\s+Code\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		codes[m[1]] = true
	}
	return codes
}

// goType is a struct's JSON field names, including embedded structs.
type goType struct {
	fields map[string]bool
}

// tsInterface is an exported interface's own fields and extends clause.
type tsInterface struct {
	fields  []string
	extends []string
}

// renamedTypes maps a console interface to a Go type with a different name:
// the console names its request bodies after what they are for, and the
// service and docs packages name them after the operation or the page.
var renamedTypes = map[string]string{
	"ClusterUpdate":       "UpdateClusterRequest",
	"DNSProfileInput":     "UpdateDNSProfileRequest",
	"DocsGroup":           "NavGroup",
	"DocsItem":            "NavItem",
	"DocsNav":             "Nav",
	"DocsPage":            "Page",
	"OrganizationUpdate":  "UpdateOrganizationRequest",
	"PlanInput":           "UpdatePlanRequest",
	"SettingsUpdate":      "UpdateSettingsRequest",
	"SetupLinkInput":      "CreateSetupLinkRequest",
	"StorageProfileInput": "UpdateStorageProfileRequest",
	"SubscriptionInput":   "SetSubscriptionRequest",
}

// TestInterfaceFieldsExist fails when a field in the console's interfaces is
// not what the API sends: the JSON field names must match the Go json tags.
// The check is one-way: the API may send fields the console does not read.
func TestInterfaceFieldsExist(t *testing.T) {
	ts := parseTSInterfaces(t)
	if len(ts) < 30 {
		t.Fatalf("parsed only %d console interfaces; has api.ts changed shape?", len(ts))
	}
	goTypes := parseGoTypes(t)
	if len(goTypes) < 30 {
		t.Fatalf("parsed only %d Go structs; have internal/service and internal/docs changed shape?", len(goTypes))
	}

	names := make([]string, 0, len(ts))
	for name := range ts {
		names = append(names, name)
	}
	sort.Strings(names)

	pairs, fields := 0, 0
	for _, name := range names {
		goName, ok := renamedTypes[name]
		if !ok {
			goName = name
		}
		typ, ok := goTypes[goName]
		if !ok {
			continue
		}
		pairs++
		for _, field := range allTSFields(name, ts, nil) {
			if !typ.fields[field] {
				t.Errorf("%s.%s has no matching json tag in Go struct %s", name, field, goName)
			} else {
				fields++
			}
		}
	}
	if pairs < 30 {
		t.Fatalf("matched only %d console interfaces to Go structs; expected at least 30", pairs)
	}
	if fields < 150 {
		t.Fatalf("matched only %d fields; expected at least 150", fields)
	}
}

// allTSFields returns an interface's fields, including those it inherits.
func allTSFields(name string, ts map[string]tsInterface, seen map[string]bool) []string {
	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[name] {
		return nil
	}
	seen[name] = true
	iface := ts[name]
	out := append([]string(nil), iface.fields...)
	for _, parent := range iface.extends {
		out = append(out, allTSFields(parent, ts, seen)...)
	}
	return out
}

// parseTSInterfaces reads every exported interface in api.ts and its fields.
func parseTSInterfaces(t *testing.T) map[string]tsInterface {
	t.Helper()
	src, err := os.ReadFile("src/lib/api.ts")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	start := regexp.MustCompile(`^export interface (\w+)(?: extends ([\w, ]+))? \{`)
	out := make(map[string]tsInterface)
	for i := 0; i < len(lines); {
		m := start.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		iface := tsInterface{}
		if m[2] != "" {
			for _, parent := range strings.Split(m[2], ",") {
				if parent = strings.TrimSpace(parent); parent != "" {
					iface.extends = append(iface.extends, parent)
				}
			}
		}
		depth := braceDepth(lines[i])
		j := i + 1
		for ; j < len(lines) && depth > 0; j++ {
			if depth == 1 {
				if field := fieldName(lines[j]); field != "" {
					iface.fields = append(iface.fields, field)
				}
			}
			depth += braceDepth(lines[j])
		}
		out[m[1]] = iface
		i = j
	}
	return out
}

var fieldLine = regexp.MustCompile(`^\s+([a-zA-Z_]\w*)\??\s*:`)

func fieldName(line string) string {
	m := fieldLine.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1]
}

func braceDepth(line string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth
}

// parseGoTypes reads the JSON field names of every struct in the service and
// in internal/docs, including embedded structs.
func parseGoTypes(t *testing.T) map[string]goType {
	t.Helper()
	structs := make(map[string]*ast.StructType)
	for _, dir := range []string{"../internal/service", "../internal/docs"} {
		for name, st := range parseStructs(t, dir) {
			structs[name] = st
		}
	}
	out := make(map[string]goType, len(structs))
	for name := range structs {
		fields := make(map[string]bool)
		collectJSONFields(structs, name, fields, map[string]bool{})
		out[name] = goType{fields: fields}
	}
	return out
}

func parseStructs(t *testing.T, dir string) map[string]*ast.StructType {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	structs := make(map[string]*ast.StructType)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if st, ok := typeSpec.Type.(*ast.StructType); ok {
						structs[typeSpec.Name.Name] = st
					}
				}
			}
		}
	}
	return structs
}

var jsonTagName = regexp.MustCompile(`json:"([^",]+)`)

func collectJSONFields(structs map[string]*ast.StructType, name string, out map[string]bool, seen map[string]bool) {
	if seen[name] {
		return
	}
	seen[name] = true
	st, ok := structs[name]
	if !ok {
		return
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			if ident, ok := field.Type.(*ast.Ident); ok {
				collectJSONFields(structs, ident.Name, out, seen)
			}
			continue
		}
		if field.Tag == nil {
			continue
		}
		tag := strings.Trim(field.Tag.Value, "`")
		m := jsonTagName.FindStringSubmatch(tag)
		if m == nil || m[1] == "-" {
			continue
		}
		out[m[1]] = true
	}
}
