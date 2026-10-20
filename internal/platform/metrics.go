package platform

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsNamespace — префикс имён метрик платформы.
const metricsNamespace = "platform"

// Metrics — реестр метрик сервиса: константная метка service и стандартные
// коллекторы рантайма и процесса.
type Metrics struct {
	registry   *prometheus.Registry
	registerer prometheus.Registerer
}

// NewRegistry создаёт реестр Prometheus: коллекторы Go и процесса,
// все метрики сервиса получают константную метку service.
func NewRegistry(service string) *Metrics {
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(prometheus.Labels{"service": service}, registry)
	registerer.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return &Metrics{registry: registry, registerer: registerer}
}

// Handler возвращает обработчик экспозиции метрик (endpoint /metrics).
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// MustRegister регистрирует пользовательские коллекторы сервиса
// (с константной меткой service).
func (m *Metrics) MustRegister(collectors ...prometheus.Collector) {
	m.registerer.MustRegister(collectors...)
}

// HTTPServerMetrics — метрики HTTP-сервера: счётчик ответов и гистограмма
// длительности запросов по методу, пути и коду ответа.
type HTTPServerMetrics struct {
	requestsTotal *prometheus.CounterVec
	duration      *prometheus.HistogramVec
}

// HTTPServerMetrics создаёт и регистрирует метрики HTTP-сервера в реестре.
func (m *Metrics) HTTPServerMetrics() *HTTPServerMetrics {
	metrics := &HTTPServerMetrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "http_requests_total",
			Help:      "Количество обработанных HTTP-запросов по методу, пути и коду ответа.",
		}, []string{"method", "path", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "http_request_duration_seconds",
			Help:      "Длительность обработки HTTP-запросов в секундах.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "path", "code"}),
	}
	m.registerer.MustRegister(metrics.requestsTotal, metrics.duration)
	return metrics
}

// HTTPMiddleware измеряет каждый запрос обработчика. Нормализация пути —
// ответственность вызывающего: labeler возвращает метку path (например шаблон
// маршрута); nil labeler использует сырой r.URL.Path.
func (m *HTTPServerMetrics) HTTPMiddleware(labeler func(r *http.Request) string) func(http.Handler) http.Handler {
	if labeler == nil {
		labeler = func(r *http.Request) string { return r.URL.Path }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
			started := time.Now()
			next.ServeHTTP(recorder, r)
			labels := prometheus.Labels{
				"method": r.Method,
				"path":   labeler(r),
				"code":   strconv.Itoa(recorder.code),
			}
			m.requestsTotal.With(labels).Inc()
			m.duration.With(labels).Observe(time.Since(started).Seconds())
		})
	}
}

// statusRecorder перехватывает код ответа обработчика.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

// WriteHeader фиксирует код ответа и делегирует запись.
func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap отдаёт исходный ResponseWriter для http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
