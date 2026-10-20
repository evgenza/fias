// Package static — статика address-api, встроенная в бинарь: Swagger UI
// (swagger-ui-dist 5.34.0, завендоренные файлы), демонстрационный веб-клиент
// и копия спецификации OpenAPI (канонический файл — api/openapi/address-api.yaml;
// синхронизация копии проверяется sync_test.go).
package static

import "embed"

// FS — все статические файлы сервиса.
//
//go:embed swagger-ui web openapi.yaml
var FS embed.FS
