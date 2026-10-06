// Команда gar-importer — загрузчик файловой выгрузки ГАР (ФИАС) в PostgreSQL.
//
// Подготовительный этап: описаны модели всех 22 форматов выгрузки
// (internal/model), схема базы (migrations) и разбор имён файлов
// (internal/garxml). Далее — потоковое чтение XML из zip-архива
// и массовая загрузка в PostgreSQL.
package main

import (
	"fmt"
	"os"
)

const usage = `gar-importer — загрузчик выгрузки ГАР (ФИАС) в PostgreSQL

Использование:
  gar-importer <команда> [аргументы]

Команды:
  version  — версия сборки
  import   — разбор zip-архива выгрузки и загрузка в PostgreSQL (в разработке)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("gar-importer dev")
	case "import":
		fmt.Fprintln(os.Stderr, "import: ещё не реализовано — этап потокового парсера XML и загрузчика PostgreSQL")
		os.Exit(1)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
