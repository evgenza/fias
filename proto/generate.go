// Package proto фиксирует точку входа go generate для регенерации
// protobuf-кода сервисов платформы (см. README.md в этом каталоге).
package proto

// Плагины protoc-gen-go и protoc-gen-go-grpc собираются из
// tool-зависимостей модуля в ../bin (каталог игнорируется git), buf
// подхватывает их из PATH.
//go:generate go build -o ../bin/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go
//go:generate go build -o ../bin/protoc-gen-go-grpc google.golang.org/grpc/cmd/protoc-gen-go-grpc
// buf запускается из корня модуля: именно там лежат buf.yaml и buf.gen.yaml,
// а `out: proto` шаблона отсчитывается от текущего каталога. Переменные
// окружения в директиве не используются: go generate раскрывает $PWD/$PATH
// до запуска оболочки, что ломает пути с пробелами.
//go:generate sh -c "cd .. && PATH=bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin go tool buf generate"
