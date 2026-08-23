package tuiproto_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const regenerateHint = "run `go run scripts/gen-tui-protocol.go` to regenerate protocol output"

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func TestGeneratedGoTypesMatchSchema(t *testing.T) {
	document, err := tuiproto.LoadDocument()
	if err != nil {
		t.Fatalf("LoadDocument() error = %v", err)
	}
	rendered, err := tuiproto.RenderGo(document)
	if err != nil {
		t.Fatalf("RenderGo() error = %v", err)
	}
	assertGeneratedFileMatches(t, tuiproto.GoOutputPath, rendered)
}

func TestGeneratedTypeScriptTypesMatchSchema(t *testing.T) {
	document, err := tuiproto.LoadDocument()
	if err != nil {
		t.Fatalf("LoadDocument() error = %v", err)
	}
	rendered, err := tuiproto.RenderTypeScript(document)
	if err != nil {
		t.Fatalf("RenderTypeScript() error = %v", err)
	}
	assertGeneratedFileMatches(t, tuiproto.TypeScriptOutputPath, rendered)
}

func assertGeneratedFileMatches(t *testing.T, relative string, want []byte) {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), filepath.FromSlash(relative))
	got, err := os.ReadFile(path) // #nosec G304 -- fixed repository-relative generated path
	if err != nil {
		t.Fatalf("read generated %s: %v (%s)", relative, err, regenerateHint)
	}
	if string(got) != string(want) {
		t.Fatalf("generated %s is stale; %s", relative, regenerateHint)
	}
}

func TestProtocolDocumentDeclaresSessionContract(t *testing.T) {
	document, err := tuiproto.LoadDocument()
	if err != nil {
		t.Fatalf("LoadDocument() error = %v", err)
	}
	if document.Protocol.Version != tuiproto.ProtocolVersion {
		t.Fatalf("schema protocol version = %q, generated constant = %q", document.Protocol.Version, tuiproto.ProtocolVersion)
	}
	if document.Protocol.Version != "tui/v2" {
		t.Fatalf("schema protocol version = %q, want tui/v2 for required authentication readiness", document.Protocol.Version)
	}
	if document.Protocol.BasePath != tuiproto.ProtocolBasePath {
		t.Fatalf("schema base path = %q, generated constant = %q", document.Protocol.BasePath, tuiproto.ProtocolBasePath)
	}
	for _, required := range []string{"AuthStatus", "Bootstrap", "Health", "CourseList", "Event", "Operation", "Problem"} {
		if _, ok := document.Defs[required]; !ok {
			t.Fatalf("protocol schema is missing required definition %s", required)
		}
	}
	health := document.Defs["Health"]
	if _, ok := health.Properties["authStatus"]; !ok {
		t.Fatal("protocol Health is missing required authStatus")
	}
}

func TestParseDocumentRejectsUnsupportedSchemas(t *testing.T) {
	for name, schema := range map[string]string{
		"missing protocol":     `{"$defs":{"A":{"type":"string","enum":["a"]}}}`,
		"missing definitions":  `{"x-protocol":{"version":"tui/v1","basePath":"/tui/v1","headers":{"a":"A"}},"$defs":{}}`,
		"unsupported type":     `{"x-protocol":{"version":"tui/v1","basePath":"/tui/v1","headers":{"a":"A"}},"$defs":{"A":{"type":"object","properties":{"b":{"type":"null"}}}}}`,
		"dangling reference":   `{"x-protocol":{"version":"tui/v1","basePath":"/tui/v1","headers":{"a":"A"}},"$defs":{"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/Missing"}}}}}`,
		"unknown required key": `{"x-protocol":{"version":"tui/v1","basePath":"/tui/v1","headers":{"a":"A"}},"$defs":{"A":{"type":"object","required":["c"],"properties":{"b":{"type":"string"}}}}}`,
		"array without items":  `{"x-protocol":{"version":"tui/v1","basePath":"/tui/v1","headers":{"a":"A"}},"$defs":{"A":{"type":"object","properties":{"b":{"type":"array"}}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tuiproto.ParseDocument([]byte(schema)); err == nil {
				t.Fatal("ParseDocument() accepted an unsupported schema")
			}
		})
	}
}

func TestRenderGoUsesRepositoryInitialisms(t *testing.T) {
	document, err := tuiproto.LoadDocument()
	if err != nil {
		t.Fatalf("LoadDocument() error = %v", err)
	}
	rendered, err := tuiproto.RenderGo(document)
	if err != nil {
		t.Fatalf("RenderGo() error = %v", err)
	}
	source := string(rendered)
	for _, want := range []string{"SessionID string", "InstituteID int64", "OperationID *string"} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated Go source is missing %q", want)
		}
	}
	if strings.Contains(source, "SessionId") || strings.Contains(source, "InstituteId") {
		t.Fatal("generated Go source uses non-idiomatic Id suffixes")
	}
}
