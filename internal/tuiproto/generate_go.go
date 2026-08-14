package tuiproto

import (
	"fmt"
	"go/format"
	"strings"
)

// GoPackageName is the package that owns the generated Go protocol types.
const GoPackageName = "tuiproto"

// GoOutputPath is the repository-relative destination of the generated Go
// definitions.
const GoOutputPath = "internal/tuiproto/types_gen.go"

// RenderGo projects the protocol document into gofmt-formatted Go source.
func RenderGo(document Document) ([]byte, error) {
	lines := []string{
		"// Code generated from protocol.schema.json. DO NOT EDIT.",
		"",
		"package " + GoPackageName,
		"",
	}
	lines = append(lines, renderGoProtocolConstants(document)...)
	for _, name := range document.DefinitionNames() {
		lines = append(lines, "")
		definition := document.Defs[name]
		if definition.IsEnum() {
			lines = append(lines, renderGoEnum(name, definition)...)
			continue
		}
		lines = append(lines, renderGoStruct(name, definition)...)
	}
	source := strings.Join(lines, "\n") + "\n"
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("format generated Go protocol source: %w", err)
	}
	return formatted, nil
}

func renderGoProtocolConstants(document Document) []string {
	lines := []string{"const ("}
	lines = append(lines,
		"\t// ProtocolVersion is the protocol identity every session request must declare.",
		fmt.Sprintf("\tProtocolVersion = %q", document.Protocol.Version),
		"",
		"\t// ProtocolBasePath is the versioned public path prefix of the session contract.",
		fmt.Sprintf("\tProtocolBasePath = %q", document.Protocol.BasePath),
	)
	for _, key := range document.HeaderNames() {
		name := goExportedName(key) + "Header"
		lines = append(lines, "")
		lines = append(lines, docComment("\t//", name, "is a session protocol header name.")...)
		lines = append(lines, fmt.Sprintf("\t%s = %q", name, document.Protocol.Headers[key]))
	}
	return append(lines, ")")
}

func renderGoEnum(name string, definition Definition) []string {
	lines := docComment("//", name, definition.Description)
	lines = append(lines, fmt.Sprintf("type %s string", name), "", "const (")
	for _, value := range definition.Enum {
		constant := name + goConstName(value)
		lines = append(lines,
			fmt.Sprintf("\t// %s is the %q %s value.", constant, value, name),
			fmt.Sprintf("\t%s %s = %q", constant, name, value),
		)
	}
	return append(lines, ")")
}

func renderGoStruct(name string, definition Definition) []string {
	lines := docComment("//", name, definition.Description)
	lines = append(lines, fmt.Sprintf("type %s struct {", name))
	for index, property := range sortedPropertyNames(definition) {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, renderGoField(definition, property)...)
	}
	return append(lines, "}")
}

func renderGoField(definition Definition, property string) []string {
	schema := definition.Properties[property]
	required := isRequired(definition, property)
	lines := docComment("\t//", "", schema.Description)
	if !required {
		lines = append(lines, "\t// This property is optional and absent when not applicable.")
	}
	tag := property
	goType := goPropertyType(schema)
	if !required {
		goType = "*" + goType
		tag += ",omitempty"
	}
	return append(lines, fmt.Sprintf("\t%s %s `json:%q`", goExportedName(property), goType, tag))
}

func goPropertyType(property Property) string {
	if property.Ref != "" {
		return refName(property.Ref)
	}
	switch property.Type {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		return "[]" + goPropertyType(*property.Items)
	default:
		return "any"
	}
}
