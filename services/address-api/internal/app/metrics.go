package app

import (
	"github.com/prometheus/client_golang/prometheus"

	"gar-importer/internal/platform"
)

// metrics — метрики сервиса.
type metrics struct {
	suggest    *prometheus.CounterVec
	cache      *prometheus.CounterVec
	dependency *prometheus.GaugeVec
}

// newMetrics создаёт и регистрирует метрики address-api.
func newMetrics(registry *platform.Metrics) *metrics {
	m := &metrics{
		suggest: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "address_api_suggest_total",
			Help: "Количество запросов подсказок по результату (ok, invalid, unavailable, error).",
		}, []string{"result"}),
		cache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "address_cache_total",
			Help: "Количество обращений к кэшу карточек по результату (hit, miss, error).",
		}, []string{"result"}),
		dependency: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "address_api_dependency_up",
			Help: "Состояние необязательных зависимостей (redis, suggester): 1 — доступна, 0 — нет.",
		}, []string{"dependency"}),
	}
	registry.MustRegister(m.suggest, m.cache, m.dependency)
	return m
}
