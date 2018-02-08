package maker

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	"golang.org/x/tools/imports"
)

// Maker generates interfaces from structs.
type Maker struct {
	// StructName is the name of the struct from which to generate an interface.
	StructName string
	// If CopyDocs is true, doc comments will be copied to the generated interface.
	CopyDocs bool

	fset *token.FileSet

	importsByPath        map[string]*importedPkg
	importsByAlias       map[string]*importedPkg
	imports              []*importedPkg
	methods              []*method
	methodNames          map[string]struct{}
	srcPackage           string
	omitGeneratedComment bool
}

// errorAlias formats the alias for error messages.
// It replaces an empty string with "<none>".
func errorAlias(alias string) string {
	if alias == "" {
		return "<none>"
	}
	return alias
}

func (m *Maker) init() {
	if m.fset == nil {
		m.fset = token.NewFileSet()
	}
	if m.importsByPath == nil {
		m.importsByPath = make(map[string]*importedPkg)
	}
	if m.importsByAlias == nil {
		m.importsByAlias = make(map[string]*importedPkg)
	}
	if m.methods == nil {
		m.methodNames = make(map[string]struct{})
	}
}

func (m *Maker) AddImport(alias, path string) {
	i := &importedPkg{Alias: alias, Path: path}
	m.imports = append(m.imports, i)
}

func (m *Maker) SourcePackage(p string) {
	m.srcPackage = p
}

func (m *Maker) OmitGeneratedComment() {
	m.omitGeneratedComment = true
}

func (m *Maker) parseDeclarations(astFile *ast.File) (hasMethods bool, err error) {
	for _, d := range astFile.Decls {

		var a string
		var fd *ast.FuncDecl

		if a, fd = m.getReceiverTypeName(d); a != m.StructName {
			continue
		}

		if !fd.Name.IsExported() {
			continue
		}

		hasMethods = true
		methodName := fd.Name.String()
		if _, ok := m.methodNames[methodName]; ok {
			continue
		}

		method := &method{Docs: []string{}}

		params, err := m.printParameters(fd.Type.Params)
		if err != nil {
			return hasMethods, errors.Wrap(err, "failed printing parameters")
		}
		ret, err := m.printParameters(fd.Type.Results)
		if err != nil {
			return hasMethods, errors.Wrap(err, "failed printing return values")
		}
		method.Code = fmt.Sprintf("%s(%s) (%s)", methodName, params, ret)

		if fd.Doc != nil && m.CopyDocs {
			for _, d := range fd.Doc.List {
				method.Docs = append(method.Docs, d.Text)
			}
		}

		m.methodNames[methodName] = struct{}{}
		m.methods = append(m.methods, method)
	}
	return
}

func (m *Maker) parseImports(a *ast.File) error {
	for _, i := range a.Imports {
		alias := ""
		if i.Name != nil {
			alias = i.Name.String()
		}
		if alias == "." {
			// Ignore dot imports.
			// Without parsing all the imported packages, we can't figure out
			// which ones are used by the interface, and which ones are not.
			// Goimports can't do this either.
			// However, we can't throw an error just because we find a
			// dot import when we're parsing all the files in a directory.
			// Let's assume that the struct we're building an
			// interface from doesn't use types from the dot import,
			// and everything will be fine.
			continue
		}
		path, err := strconv.Unquote(i.Path.Value)
		if err != nil {
			return errors.Wrapf(err, "parsing import `%v` failed", i.Path.Value)
		}
		if existing, ok := m.importsByPath[path]; ok && existing.Alias != alias {
			// It would be possible to pick one alias and rewrite all the types,
			// but that would require parsing all the imports to find the correct
			// package name (which might differ from the import path's last element),
			// and that would require correctly finding the package in GOPATH
			// or vendor directories.
			format := "package %q imported multiple times with different aliases: %v, %v"
			return fmt.Errorf(format, path, errorAlias(existing.Alias), errorAlias(alias))
		} else if !ok {
			if alias != "" {
				if _, ok := m.importsByAlias[alias]; ok {
					return fmt.Errorf("import alias %v already in use", alias)
				}
			}
			imp := &importedPkg{
				Path:  path,
				Alias: alias,
			}
			m.importsByPath[path] = imp
			m.importsByAlias[alias] = imp
			m.imports = append(m.imports, imp)
		}
	}
	return nil
}

// ParseSource parses the source code in src.
// filename is used for position information only.
func (m *Maker) ParseDeclarations(src []byte, filename string) (declarations map[string]int32, err error) {
	m.init()

	declarations = make(map[string]int32)
	a, err := parser.ParseFile(m.fset, filename, src, parser.ParseComments)
	if err != nil {
		return declarations, errors.Wrap(err, "parsing file failed")
	}
	for _, d := range a.Decls {
		a, _ := m.getReceiverTypeName(d)
		declarations[a]++
	}
	return
}

// ParseSource parses the source code in src.
// filename is used for position information only.
func (m *Maker) ParseSource(src []byte, filename string) error {
	m.init()

	a, err := parser.ParseFile(m.fset, filename, src, parser.ParseComments)
	if err != nil {
		return errors.Wrap(err, "parsing file failed")
	}
	hasMethods, err := m.parseDeclarations(a)
	if err != nil {
		return err
	}

	// No point checking imports if there are no relevant methods in this file.
	// This also avoids throwing unnecessary errors about imports in files that
	// are not relevant.
	if !hasMethods {
		return nil
	}

	err = m.parseImports(a)
	if err != nil {
		return err
	}

	return nil
}

func (m *Maker) makeInterface(pkgName, ifaceName string) string {
	var output []string
	if !m.omitGeneratedComment {
		output = append(output, "// Code generated by ifacemaker. DO NOT EDIT.")
	}
	output = append(output, "")
	output = append(output, "package "+pkgName)
	output = append(output, "import (")
	for _, pkgImport := range m.imports {
		output = append(output, pkgImport.Lines()...)
	}
	output = append(output, ")")
	if m.srcPackage != "" {
		output = append(output,
			fmt.Sprintf("var _ %s = (*%s.%s)(nil)", ifaceName, m.srcPackage, m.StructName),
		)
	}
	output = append(output,
		fmt.Sprintf("type %s interface {", ifaceName),
	)
	for _, method := range m.methods {
		output = append(output, method.Lines()...)
	}
	output = append(output, "}")

	return strings.Join(output, "\n")
}

// MakeInterface creates the go file with the generated interface.
// The package will be named pkgName, and the interface will be named ifaceName.
func (m *Maker) MakeInterface(pkgName, ifaceName string) ([]byte, error) {
	unformatted := m.makeInterface(pkgName, ifaceName)
	b, err := formatCode(unformatted)
	if err != nil {
		err = errors.Wrapf(err, "Failed to format generated code. This could be a bug in ifacemaker. The generated code was:\n%v\nError", unformatted)
	}
	return b, err
}

// import resolution: sort imports by number of aliases.
// sort aliases by length ("" is unaliased).
// try all aliases. if all are already used up, generate a free one: pkgname + n,
// where n is a number so that the alias is free.

type method struct {
	Code string
	Docs []string
}

type importedPkg struct {
	Alias string
	Path  string
}

func (m *method) Lines() []string {
	var lines []string
	lines = append(lines, m.Docs...)
	lines = append(lines, m.Code)
	return lines
}

func (i *importedPkg) Lines() []string {
	var lines []string
	lines = append(lines, fmt.Sprintf("%v %q", i.Alias, i.Path))
	return lines
}

func (m *Maker) getReceiverTypeName(fl interface{}) (string, *ast.FuncDecl) {
	fd, ok := fl.(*ast.FuncDecl)
	if !ok {
		return "", nil
	}
	if fd.Recv.NumFields() != 1 {
		return "", nil
	}
	t := fd.Recv.List[0].Type
	if st, stok := t.(*ast.StarExpr); stok {
		t = st.X
	}

	ident, ok := t.(*ast.Ident)
	if !ok {
		return "", nil
	}
	return ident.Name, fd

}

func (m *Maker) printParameters(fl *ast.FieldList) (string, error) {
	if fl == nil {
		return "", nil
	}
	buff := &bytes.Buffer{}
	ll := len(fl.List)
	for ii, field := range fl.List {
		l := len(field.Names)
		for i, name := range field.Names {
			err := printer.Fprint(buff, m.fset, name)
			if err != nil {
				return "", errors.Wrap(err, "failed printing parameter name")
			}
			if i < l-1 {
				fmt.Fprint(buff, ",")
			} else {
				fmt.Fprint(buff, " ")
			}
		}

		typeBuff := &bytes.Buffer{}
		err := printer.Fprint(typeBuff, m.fset, field.Type)
		if err != nil {
			return "", errors.Wrap(err, "failed printing parameter type")
		}
		buff.Write(m.replaceType(typeBuff).Bytes())
		if ii < ll-1 {
			fmt.Fprint(buff, ",")
		}
	}

	return buff.String(), nil
}

func (m *Maker) replaceTypeOld(in *bytes.Buffer) *bytes.Buffer {
	if m.srcPackage == "" {
		return in
	}

	s := in.String()

	// No uppercase then this is not exported
	if !strings.ContainsAny(s, "ABCDEFGHIGKLMNOPQRSTUVWXYZ") {
		return in
	}
	// Already qualified with a package prefix
	if strings.Contains(s, ".") {
		return in
	}

	funcPrefix, s := removePrefix(s, "func(")
	chanPrefix, s := removePrefix(s, "chan ", "<-chan ", "chan<- ")

	ptrPrefix, s := removePrefix(s, "*")

	// prepend the source package to the exported type
	s = funcPrefix + chanPrefix + ptrPrefix + m.srcPackage + "." + s

	return bytes.NewBufferString(s)
}

func (m *Maker) replaceType(in *bytes.Buffer) (out *bytes.Buffer) {
	if m.srcPackage == "" {
		return in
	}
	out = &bytes.Buffer{}
	var err error
	var r rune
	var p rune

	// i've got 99 problems but regex isn't one of them

	// https://golang.org/ref/spec#Identifiers
	isValidTypeRune := func(r rune) bool {
		return r == rune('_') || unicode.IsLetter(r) || unicode.IsDigit(r)
	}

	addPrefix := func(current rune, previous rune) bool {
		// for already qualified
		if previous == rune('.') {
			return false
		}
		// continuing an identifier
		if isValidTypeRune(previous) {
			return false
		}
		// not part of an identifier
		if !isValidTypeRune(current) {
			return false
		}
		// only exported
		// https://golang.org/ref/spec#Exported_identifiers
		return unicode.IsUpper(current)
	}

	for r, _, err = in.ReadRune(); err != io.EOF; {
		if addPrefix(r, p) {
			out.WriteString(m.srcPackage + ".")
		}
		out.WriteRune(r)
		p = r
		r, _, err = in.ReadRune()
	}

	return out
}

func removePrefix(s string, variations ...string) (removed string, remainder string) {
	for _, pfx := range variations {
		if strings.HasPrefix(s, pfx) {
			return pfx, strings.TrimPrefix(s, pfx)
		}
	}
	return "", s
}

func formatCode(code string) ([]byte, error) {
	opts := &imports.Options{
		TabIndent: true,
		TabWidth:  2,
		Fragment:  true,
		Comments:  true,
	}
	return imports.Process("", []byte(code), opts)
}

func (m *Maker) GetGoFiles(paths ...string) (allFiles []string, err error) {

	var noFiles []string

	for _, f := range paths {
		fi, err := os.Stat(f)
		if err != nil {
			return noFiles, err
		}
		if fi.IsDir() {
			dir, err := os.Open(f)
			if err != nil {
				return noFiles, err
			}
			dirFiles, err := dir.Readdir(-1)
			dir.Close()
			if err != nil {
				return noFiles, err
			}
			var dirFileNames []string
			for _, fi := range dirFiles {
				if !fi.IsDir() && strings.HasSuffix(fi.Name(), ".go") {
					dirFileNames = append(dirFileNames, filepath.Join(f, fi.Name()))
				}
			}
			sort.Strings(dirFileNames)
			allFiles = append(allFiles, dirFileNames...)
		} else {
			allFiles = append(allFiles, f)
		}
	}
	return allFiles, err
}

func (m *Maker) ParseFiles(files ...string) error {
	for _, f := range files {
		src, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}
		err = m.ParseSource(src, filepath.Base(f))
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Maker) ReadStructs(files ...string) (allStructs map[string]int32, err error) {
	allFiles, err := m.GetGoFiles(files...)
	if err != nil {
		return allStructs, err
	}

	allStructs = make(map[string]int32)

	for _, f := range allFiles {
		src, err := ioutil.ReadFile(f)
		if err != nil {
			return allStructs, err
		}

		st, err := m.ParseDeclarations(src, filepath.Base(f))
		if err != nil {
			return allStructs, err
		}

		for k, v := range st {
			allStructs[k] += v
		}
	}

	return
}
