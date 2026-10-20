package static_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gar-importer/services/address-api/static"
)

// TestOpenAPISpecInSync проверяет, что встроенная в бинарь копия спецификации
// совпадает с каноническим файлом api/openapi/address-api.yaml.
func TestOpenAPISpecInSync(t *testing.T) {
	embedded, err := static.FS.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("чтение встроенной спецификации: %v", err)
	}
	canonical, err := os.ReadFile("../../../api/openapi/address-api.yaml")
	if err != nil {
		t.Fatalf("чтение канонической спецификации: %v", err)
	}
	if !bytes.Equal(embedded, canonical) {
		t.Fatal("static/openapi.yaml расходится с api/openapi/address-api.yaml: синхронизируйте копию")
	}
}

// Parse the shipped contract as YAML and check response references. Byte equality
// alone would also accept two identical but malformed specifications.
func TestOpenAPIContract(t *testing.T) {
	data, err := static.FS.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Paths   map[string]struct {
			Get struct {
				Responses map[string]struct {
					Ref string `yaml:"$ref"`
				} `yaml:"responses"`
			} `yaml:"get"`
		} `yaml:"paths"`
		Components struct {
			Responses  map[string]any `yaml:"responses"`
			Parameters map[string]struct {
				Schema struct {
					Minimum int `yaml:"minimum"`
				} `yaml:"schema"`
			} `yaml:"parameters"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.0.3" || spec.Components.Parameters["ObjectID"].Schema.Minimum != 1 {
		t.Fatal("invalid OpenAPI/ObjectID contract")
	}
	for path, item := range spec.Paths {
		for code, response := range item.Get.Responses {
			if response.Ref != "" {
				name := strings.TrimPrefix(response.Ref, "#/components/responses/")
				if _, ok := spec.Components.Responses[name]; !ok {
					t.Errorf("%s %s: unresolved %s", path, code, response.Ref)
				}
			}
		}
	}
}
