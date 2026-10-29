package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"text/scanner"
)

// Documents 09 and 13 are the protocol source of truth. Check schema tokens,
// including field numbers and stream directions, independently of generated
// source drift; formatting and explanatory comments may differ.
func TestDocumentedProtocolMatchesSources(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	blocks := regexp.MustCompile("(?s)```protobuf\\s*\\n(.*?)\\n```")
	for _, contract := range []struct {
		doc   string
		files []string
	}{
		{"09-protocol-definition.md", []string{"shiki.proto", "types.proto", "plugin.proto"}},
		{"13-local-ipc-protocol.md", []string{"agent.proto"}},
	} {
		data, err := os.ReadFile(filepath.Join(root, "docs", contract.doc))
		if err != nil {
			t.Fatal(err)
		}
		schema := blocks.FindAllSubmatch(data, -1)
		if len(schema) != len(contract.files) {
			t.Fatalf("%s must contain %d protocol definitions", contract.doc, len(contract.files))
		}
		for index, name := range contract.files {
			t.Run(name, func(t *testing.T) {
				actual, err := os.ReadFile(filepath.Join(root, "proto", name))
				if err != nil {
					t.Fatal(err)
				}
				documented := strings.ReplaceAll(string(schema[index][1]), "github.com/yourusername/shikigami", module)
				expectedTokens := protocolTokens(t, documented)
				actualTokens := protocolTokens(t, string(actual))
				for token := 0; token < len(expectedTokens) && token < len(actualTokens); token++ {
					if expectedTokens[token] != actualTokens[token] {
						t.Fatalf("%s differs from docs/%s at token %d: expected %q, got %q", name, contract.doc, token, expectedTokens[token], actualTokens[token])
					}
				}
				if len(expectedTokens) != len(actualTokens) {
					t.Fatalf("%s schema length differs from docs/%s", name, contract.doc)
				}
			})
		}
	}
}
func protocolTokens(t *testing.T, source string) []string {
	t.Helper()
	var lexer scanner.Scanner
	lexer.Init(strings.NewReader(source))
	lexer.Error = func(_ *scanner.Scanner, message string) { t.Errorf("invalid protocol text: %s", message) }
	var tokens []string
	for next := lexer.Scan(); next != scanner.EOF; next = lexer.Scan() {
		tokens = append(tokens, lexer.TokenText())
	}
	return tokens
}
