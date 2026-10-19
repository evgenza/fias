// Команда gar-gen-spec генерирует internal/garxml/spec_gen.go — спецификации
// атрибутов всех форматов из закреплённых XSD каталога xsd/ репозитория.
//
//	go generate .            # из корня репозитория
//	go run ./cmd/gar-gen-spec -verify   # проверка, что файл не устарел
//
// Источник обязательности и ограничений — XSD; соответствие Ent-схемам
// проверяется тестом покрытия в internal/importer.
package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const addHouseTypes = "AS_ADDHOUSE_TYPES" // отдельного XSD нет: используется XSD AS_HOUSE_TYPES

func main() {
	xsdDir := flag.String("xsd", "xsd", "каталог с закреплёнными XSD")
	out := flag.String("out", "internal/gar/catalog_gen.go", "файл результата")
	verify := flag.Bool("verify", false, "не писать файл, только сверить содержимое")
	flag.Parse()
	data, err := generate(*xsdDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-gen-spec:", err)
		os.Exit(1)
	}
	if *verify {
		current, err := os.ReadFile(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gar-gen-spec:", err)
			os.Exit(1)
		}
		if !bytes.Equal(current, []byte(data)) {
			fmt.Fprintf(os.Stderr, "gar-gen-spec: %s устарел, запустите go generate .\n", *out)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, []byte(data), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "gar-gen-spec:", err)
		os.Exit(1)
	}
}

type node struct {
	xmlName  xml.Name
	name     string // значение атрибута name
	ref      string // значение атрибута ref
	required bool   // use="required"
	attrs    []xml.Attr
	children []*node
}

func (n *node) child(local string) *node {
	for _, c := range n.children {
		if c.local() == local {
			return c
		}
	}
	return nil
}

func (n *node) local() string { return n.xmlName.Local }

// restriction возвращает ограничения простого типа: базу и длины.
func (n *node) restriction() (base string, minLen, maxLen, length int, ok bool) {
	simple := n.child("simpleType")
	if simple == nil {
		return "", 0, 0, 0, false
	}
	rest := simple.child("restriction")
	if rest == nil {
		return "", 0, 0, 0, false
	}
	for _, attr := range rest.attrs {
		if attr.Name.Local == "base" {
			base = attr.Value
		}
	}
	for _, facet := range rest.children {
		value := 0
		for _, attr := range facet.attrs {
			if attr.Name.Local == "value" {
				fmt.Sscanf(attr.Value, "%d", &value)
			}
		}
		switch facet.local() {
		case "minLength":
			minLen = value
		case "maxLength":
			maxLen = value
		case "length":
			length = value
		}
	}
	return base, minLen, maxLen, length, true
}

// parseTree строит дерево XML из потока токенов (текст игнорируется).
func parseTree(data []byte) (*node, error) {
	root := &node{}
	stack := []*node{root}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if len(stack) == 1 {
				return root, nil
			}
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			element := &node{xmlName: token.Name, attrs: append([]xml.Attr(nil), token.Attr...)}
			for _, attr := range token.Attr {
				switch attr.Name.Local {
				case "name":
					element.name = attr.Value
				case "ref":
					element.ref = attr.Value
				case "use":
					element.required = attr.Value == "required"
				}
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, element)
			stack = append(stack, element)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// xsdFile — результат разбора одного XSD.
type xsdFile struct {
	family   string
	root     *node // элемент-обёртка с sequence
	record   *node // элемент записи
	elements map[string]*node
}

// parseXSD разбирает файл и находит корневой элемент, элемент записи и его атрибуты.
func parseXSD(file string) (xsdFile, error) {
	var parsed xsdFile
	data, err := os.ReadFile(file)
	if err != nil {
		return parsed, err
	}
	parsed.family = familyName(file)
	schema, err := parseTree(data)
	if err != nil {
		return parsed, fmt.Errorf("%s: %w", file, err)
	}
	contents := schema.child("schema")
	if contents == nil {
		return parsed, fmt.Errorf("%s: нет xs:schema", file)
	}
	parsed.elements = map[string]*node{}
	for _, element := range contents.children {
		if element.local() == "element" && element.name != "" {
			parsed.elements[element.name] = element
		}
	}
	for _, element := range contents.children {
		if element.local() != "element" {
			continue
		}
		complex := element.child("complexType")
		if complex == nil {
			continue
		}
		sequence := complex.child("sequence")
		if sequence == nil || len(sequence.children) == 0 {
			continue
		}
		parsed.root = element
		first := sequence.children[0]
		var record *node
		if first.name != "" && first.child("complexType") != nil {
			record = first // запись объявлена прямо в sequence (общий случай)
		} else {
			recordName := first.ref
			if recordName == "" {
				return parsed, fmt.Errorf("%s: элемент записи не содержит ни inline-типа, ни ref", file)
			}
			var ok bool
			if record, ok = parsed.elements[recordName]; !ok {
				return parsed, fmt.Errorf("%s: элемент записи %q не найден", file, recordName)
			}
		}
		parsed.record = record
		return parsed, nil
	}
	return parsed, fmt.Errorf("%s: не найден корневой элемент с последовательностью записи", file)
}

// attrKind сопоставляет базовый тип XSD с типом проверки.
func attrKind(base string) (string, error) {
	switch base {
	case "xs:string":
		return "AttrString", nil
	case "xs:date":
		return "AttrDate", nil
	case "xs:integer":
		return "AttrInteger", nil
	case "xs:long":
		return "AttrLong", nil
	case "xs:boolean":
		return "AttrBoolean", nil
	}
	return "", fmt.Errorf("неподдерживаемый базовый тип XSD %q", base)
}

func (n *node) base() string {
	for _, attr := range n.attrs {
		if attr.Name.Local == "type" {
			return attr.Value
		}
	}
	return ""
}

// familyName извлекает имя семейства из имени файла вида AS_HOUSES_2_251_08_04_01_01.xsd.
func familyName(file string) string {
	base := strings.TrimSuffix(filepath.Base(file), ".xsd")
	if i := strings.Index(base, "_251"); i >= 0 {
		base = base[:i]
	}
	return strings.TrimRight(base, "2_")
}

func generate(xsdDir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(xsdDir, "*.xsd"))
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("в каталоге %s нет XSD-файлов", xsdDir)
	}
	sort.Strings(files)
	parsed := make([]xsdFile, 0, len(files))
	for _, file := range files {
		schema, err := parseXSD(file)
		if err != nil {
			return "", err
		}
		parsed = append(parsed, schema)
	}
	var b strings.Builder
	b.WriteString("// Code generated by cmd/gar-gen-spec from xsd/*.xsd; DO NOT EDIT.\n")
	b.WriteString("//\n// Спецификации атрибутов форматов из закреплённых XSD.\n")
	b.WriteString("// Перегенерация: go generate . Проверка актуальности: go run ./cmd/gar-gen-spec -verify\n")
	b.WriteString("\npackage gar\n\nvar generatedSpecs = map[Kind]FormatSpec{\n")
	for _, schema := range parsed {
		spec, err := specLiteral(schema)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\t%q: %s,\n", schema.family, spec)
	}
	house, ok := parsedByFamily(parsed, "AS_HOUSE_TYPES")
	if !ok {
		return "", fmt.Errorf("для %s требуется XSD AS_HOUSE_TYPES", addHouseTypes)
	}
	spec, err := specLiteral(house)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\t%q: %s, // тот же XSD, что и AS_HOUSE_TYPES\n", addHouseTypes, spec)
	b.WriteString("}\n")
	source, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("форматирование сгенерированного кода: %w", err)
	}
	return string(source), nil
}

func parsedByFamily(parsed []xsdFile, family string) (xsdFile, bool) {
	for _, schema := range parsed {
		if schema.family == family {
			return schema, true
		}
	}
	return xsdFile{}, false
}

// specLiteral формирует литерал FormatSpec для одного XSD.
func specLiteral(f xsdFile) (string, error) {
	complex := f.record.child("complexType")
	if complex == nil {
		return "", fmt.Errorf("%s: у записи %s нет complexType", f.family, f.record.name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "{Root: %q, Record: %q, Fields: []FieldSpec{", f.root.name, f.record.name)
	for _, attr := range complex.children {
		if attr.local() != "attribute" {
			continue
		}
		kind, minLen, maxLen, length, required, err := fieldSpecOf(attr)
		if err != nil {
			return "", fmt.Errorf("%s: attribute %s: %w", f.family, attr.name, err)
		}
		fmt.Fprintf(&b, "{Name: %q, Kind: %s, Required: %v", attr.name, kind, required)
		if minLen > 0 {
			fmt.Fprintf(&b, ", MinLen: %d", minLen)
		}
		if maxLen > 0 {
			fmt.Fprintf(&b, ", MaxLen: %d", maxLen)
		}
		if length > 0 {
			fmt.Fprintf(&b, ", Length: %d", length)
		}
		b.WriteString("}, ")
	}
	b.WriteString("}}")
	return b.String(), nil
}

// fieldSpecOf вычисляет тип, ограничения и обязательность одного атрибута XSD.
func fieldSpecOf(attr *node) (kind string, minLen, maxLen, length int, required bool, err error) {
	required = attr.required
	if base := attr.base(); base != "" {
		kind, err = attrKind(base)
		return kind, 0, 0, 0, required, err
	}
	base, minLen, maxLen, length, ok := attr.restriction()
	if !ok {
		if attr.base() == "" {
			// Атрибут без типа и без restriction — xs:anySimpleType (PATH иерархий).
			return "AttrString", 0, 0, 0, required, nil
		}
		return "", 0, 0, 0, false, fmt.Errorf("нет простого типа")
	}
	kind, err = attrKind(base)
	return kind, minLen, maxLen, length, required, err
}
