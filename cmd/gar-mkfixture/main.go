// Команда gar-mkfixture собирает маленький полностью валидный ZIP-архив
// выгрузки ГАР (ФИАС) для CI-smoke и быстрого демо: корневые справочники
// и данные одного региона (по умолчанию 01) с цепочкой адреса
// регион → город → улицы → дома → квартиры → комнаты, участком
// и машино-местом. Архив проходит полный импорт gar-importer без ошибок
// валидации. Генерация детерминирована: повторный запуск с теми же
// параметрами даёт побайтно тот же файл.
//
//	go run ./cmd/gar-mkfixture --out /tmp/tiny-gar.zip [--region 1]
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "", "путь к создаваемому ZIP-архиву (обязательно)")
	region := flag.Int("region", 1, "код региона данных (1..99)")
	flag.Parse()
	if *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "gar-mkfixture: используйте --out /путь/gar-fixture.zip [--region 1]")
		os.Exit(2)
	}
	files, err := buildFixture(*region)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-mkfixture:", err)
		os.Exit(1)
	}
	if err := writeZIP(*out, files); err != nil {
		fmt.Fprintln(os.Stderr, "gar-mkfixture:", err)
		os.Exit(1)
	}
	var total int
	fmt.Fprintf(os.Stdout, "%s: файлов %d\n", *out, len(files))
	for _, file := range files {
		fmt.Fprintf(os.Stdout, "  %s: %d\n", file.name, file.records)
		total += file.records
	}
	fmt.Fprintf(os.Stdout, "записей всего: %d\n", total)
}
