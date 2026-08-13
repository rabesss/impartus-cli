package tuiproto

import (
	"fmt"
	"strings"
)

// TypeScriptOutputPath is the repository-relative destination of the generated
// TypeScript definitions consumed by the OpenTUI helper.
const TypeScriptOutputPath = "ui/src/protocol/types.gen.ts"

// RenderTypeScript projects the protocol document into TypeScript source.
func RenderTypeScript(document Document) ([]byte, error) {
	blocks := []string{strings.Join([]string{
		"// Code generated from internal/tuiproto/protocol.schema.json. DO NOT EDIT.",
		"// Regenerate with: go run scripts/gen-tui-protocol.go",
	}, "\n")}
	blocks = append(blocks, renderTypeScriptConstants(document))
	for _, name := range document.DefinitionNames() {
		definition := document.Defs[name]
		if definition.IsEnum() {
			blocks = append(blocks, renderTypeScriptUnion(name, definition))
			continue
		}
		blocks = append(blocks, renderTypeScriptInterface(name, definition))
	}
	return []byte(strings.Join(blocks, "\n\n") + "\n"), nil
}

func renderTypeScriptConstants(document Document) string {
	lines := make([]string, 0, 5+3*len(document.HeaderNames()))
	lines = append(lines,
		"/** Protocol identity every session request must declare. */",
		fmt.Sprintf("export const PROTOCOL_VERSION = %q as const", document.Protocol.Version),
		"",
		"/** Versioned public path prefix of the session contract. */",
		fmt.Sprintf("export const PROTOCOL_BASE_PATH = %q as const", document.Protocol.BasePath),
	)
	for _, key := range document.HeaderNames() {
		lines = append(lines,
			"",
			"/** Session protocol header name. */",
			fmt.Sprintf("export const %s_HEADER = %q as const", tsConstName(key), document.Protocol.Headers[key]),
		)
	}
	return strings.Join(lines, "\n")
}

func renderTypeScriptUnion(name string, definition Definition) string {
	members := make([]string, 0, len(definition.Enum))
	for _, value := range definition.Enum {
		members = append(members, fmt.Sprintf("%q", value))
	}
	lines := tsDocComment("", name, definition.Description)
	return strings.Join(append(lines, fmt.Sprintf("export type %s = %s", name, strings.Join(members, " | "))), "\n")
}

func renderTypeScriptInterface(name string, definition Definition) string {
	lines := tsDocComment("", name, definition.Description)
	lines = append(lines, fmt.Sprintf("export interface %s {", name))
	for index, property := range sortedPropertyNames(definition) {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, tsDocComment("  ", "", definition.Properties[property].Description)...)
		optional := ""
		if !isRequired(definition, property) {
			optional = "?"
		}
		lines = append(lines, fmt.Sprintf("  %s%s: %s", property, optional, tsPropertyType(definition.Properties[property])))
	}
	return strings.Join(append(lines, "}"), "\n")
}

func tsDocComment(indent, subject, description string) []string {
	body := docComment(indent+" *", subject, description)
	if len(body) == 0 {
		return nil
	}
	return append(append([]string{indent + "/**"}, body...), indent+" */")
}

func tsPropertyType(property Property) string {
	if property.Ref != "" {
		return refName(property.Ref)
	}
	switch property.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return tsPropertyType(*property.Items) + "[]"
	default:
		return "unknown"
	}
}
