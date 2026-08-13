// Package tuiproto owns the experimental OpenTUI session protocol. The
// checked-in JSON Schema document is the single source of truth: the Go DTOs
// in types_gen.go and the TypeScript definitions under ui/src/protocol are
// both projected from it, so the two processes cannot drift into parallel
// hand-written contracts.
package tuiproto

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed protocol.schema.json
var schemaJSON []byte

// SchemaJSON returns the raw protocol schema document.
func SchemaJSON() []byte {
	return append([]byte(nil), schemaJSON...)
}

// Document is the supported subset of JSON Schema used by this protocol.
type Document struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Protocol    ProtocolMetadata      `json:"x-protocol"`
	Defs        map[string]Definition `json:"$defs"`
}

// ProtocolMetadata carries the transport identity shared by both processes.
type ProtocolMetadata struct {
	Version  string            `json:"version"`
	BasePath string            `json:"basePath"`
	Headers  map[string]string `json:"headers"`
}

// Definition is one named schema: either a string enum or an object type.
type Definition struct {
	Type        string              `json:"type"`
	Description string              `json:"description"`
	Enum        []string            `json:"enum"`
	Required    []string            `json:"required"`
	Properties  map[string]Property `json:"properties"`
}

// Property is one object member.
type Property struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Ref         string    `json:"$ref"`
	Items       *Property `json:"items"`
}

// IsEnum reports whether the definition renders as a closed string union.
func (definition Definition) IsEnum() bool {
	return definition.Type == "string" && len(definition.Enum) > 0
}

// LoadDocument parses and validates the embedded protocol schema.
func LoadDocument() (Document, error) {
	return ParseDocument(schemaJSON)
}

// ParseDocument parses and validates an explicit schema document. Standard
// JSON Schema keywords that are metadata rather than generator input (notably
// $schema and $id) are accepted and ignored.
func ParseDocument(raw []byte) (Document, error) {
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return Document{}, fmt.Errorf("decode protocol schema: %w", err)
	}
	if err := document.validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (document Document) validate() error {
	if document.Protocol.Version == "" || document.Protocol.BasePath == "" {
		return errors.New("protocol schema must declare x-protocol version and basePath")
	}
	if len(document.Protocol.Headers) == 0 {
		return errors.New("protocol schema must declare x-protocol headers")
	}
	if len(document.Defs) == 0 {
		return errors.New("protocol schema must declare at least one definition")
	}
	for _, name := range document.DefinitionNames() {
		if err := document.validateDefinition(name); err != nil {
			return err
		}
	}
	return nil
}

func (document Document) validateDefinition(name string) error {
	definition := document.Defs[name]
	switch {
	case definition.IsEnum():
		return nil
	case definition.Type == "object":
		return document.validateProperties(name, definition)
	default:
		return fmt.Errorf("definition %s must be a string enum or an object", name)
	}
}

func (document Document) validateProperties(name string, definition Definition) error {
	if len(definition.Properties) == 0 {
		return fmt.Errorf("object definition %s must declare properties", name)
	}
	for _, required := range definition.Required {
		if _, ok := definition.Properties[required]; !ok {
			return fmt.Errorf("definition %s requires unknown property %s", name, required)
		}
	}
	for _, property := range sortedPropertyNames(definition) {
		if err := document.validateProperty(name, property, definition.Properties[property]); err != nil {
			return err
		}
	}
	return nil
}

func (document Document) validateProperty(definitionName, propertyName string, property Property) error {
	if property.Ref != "" {
		if _, ok := document.Defs[refName(property.Ref)]; !ok {
			return fmt.Errorf("definition %s property %s references unknown %s", definitionName, propertyName, property.Ref)
		}
		return nil
	}
	switch property.Type {
	case "string", "integer", "number", "boolean":
		return nil
	case "array":
		if property.Items == nil {
			return fmt.Errorf("definition %s property %s must declare array items", definitionName, propertyName)
		}
		return document.validateProperty(definitionName, propertyName, *property.Items)
	default:
		return fmt.Errorf("definition %s property %s has unsupported type %q", definitionName, propertyName, property.Type)
	}
}

// DefinitionNames returns every definition name in deterministic order.
func (document Document) DefinitionNames() []string {
	names := make([]string, 0, len(document.Defs))
	for name := range document.Defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HeaderNames returns the protocol header keys in deterministic order.
func (document Document) HeaderNames() []string {
	names := make([]string, 0, len(document.Protocol.Headers))
	for name := range document.Protocol.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedPropertyNames(definition Definition) []string {
	names := make([]string, 0, len(definition.Properties))
	for name := range definition.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isRequired(definition Definition, property string) bool {
	for _, required := range definition.Required {
		if required == property {
			return true
		}
	}
	return false
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/$defs/")
}
