package static_test

import (
	"bytes"
	"os"
	"testing"

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
