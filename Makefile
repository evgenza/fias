.PHONY: up down ps logs clean build test test-integration vet proto \
	seed-fixture seed-demo make-demo-zip smoke load chaos-nats chaos-es chaos-postgres

## Запуск и остановка

up:            ## Поднять всю платформу (инфраструктура + микросервисы)
	docker compose up -d --build --wait

down:          ## Остановить платформу
	docker compose down

ps:            ## Статус контейнеров
	docker compose ps

logs:          ## Логи всех сервисов
	docker compose logs -f --tail=100

clean:         ## Остановить и удалить все данные (тома)
	docker compose down -v

## Разработка

build:         ## go build всех пакетов
	go build ./...

vet:           ## go vet обоих наборов тегов
	go vet ./... && go vet -tags=integration ./...

test:          ## Юнит-тесты (без Docker)
	go test ./...

test-integration: ## Интеграционные тесты (нужен Docker)
	go test -tags=integration -p 1 -timeout 15m ./internal/... ./services/...

proto:         ## Регенерация gRPC-кода из proto/suggest/v1
	go generate ./proto/...

## Демо-данные

seed-fixture:  ## Быстрый демо-засев синтетической фикстурой (CI-дружелюбно)
	mkdir -p build && go run ./cmd/gar-mkfixture --out build/tiny-gar.zip
	scripts/seed-demo.sh build/tiny-gar.zip

make-demo-zip: ## Сборка build/gar-demo.zip (регион 77) из полного gar_xml.zip
	scripts/make-demo-zip.sh

seed-demo: make-demo-zip ## Полный демо-засев региона 77 (требует ~/Downloads/gar_xml.zip)
	scripts/seed-demo.sh build/gar-demo.zip 77

## Проверки платформы

smoke:         ## Сквозная проверка: health, REST, gRPC-зависимости, метрики
	scripts/smoke.sh

smoke-seed:    ## Сквозная проверка + полный конвейер на фикстуре
	scripts/smoke.sh --with-seed

## Нагрузка и хаос (задача с двумя звёздочками)

load:          ## Нагрузочное тестирование k6 (нужен k6 или docker)
	tests/load/run.sh

chaos-nats:    ## Хаос: остановка NATS под нагрузкой
	scripts/chaos/kill-nats.sh

chaos-es:      ## Хаос: остановка Elasticsearch под нагрузкой
	scripts/chaos/kill-elasticsearch.sh

chaos-postgres: ## Хаос: остановка PostgreSQL под нагрузкой
	scripts/chaos/kill-postgres.sh
