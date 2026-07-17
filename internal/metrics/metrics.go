package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metrics 简易 Prometheus 指标收集器
// 不依赖第三方库，手写 Prometheus 文本格式输出
type Metrics struct {
	mu        sync.Mutex
	counters  map[string]*Counter
	gauges    map[string]*Gauge
	histograms map[string]*Histogram
	startTime time.Time
}

// Counter 单调递增计数器
type Counter struct {
	value  float64
	labels map[string]string
}

// Gauge 可增可减仪表
type Gauge struct {
	value  float64
	labels map[string]string
}

// Histogram 直方图
type Histogram struct {
	buckets []float64
	counts  []uint64
	sum     float64
	count   uint64
	labels  map[string]string
}

// New 创建指标收集器
func New() *Metrics {
	return &Metrics{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
		startTime:  time.Now(),
	}
}

// IncCounter 增加计数器
func (m *Metrics) IncCounter(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	if c, ok := m.counters[key]; ok {
		c.value++
	} else {
		m.counters[key] = &Counter{value: 1, labels: labels}
	}
}

// SetGauge 设置仪表值
func (m *Metrics) SetGauge(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	if g, ok := m.gauges[key]; ok {
		g.value = value
	} else {
		m.gauges[key] = &Gauge{value: value, labels: labels}
	}
}

// Observe 记录直方图观测值
func (m *Metrics) Observe(name string, value float64, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	h, ok := m.histograms[key]
	if !ok {
		h = &Histogram{
			buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
			counts:  make([]uint64, 9),
			labels:  labels,
		}
		m.histograms[key] = h
	}
	h.sum += value
	h.count++
	for i, b := range h.buckets {
		if value <= b {
			h.counts[i]++
		}
	}
}

// metricKey 生成指标唯一键（label key 排序，避免 map 随机序导致同一逻辑指标分裂）
func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := sortedKeys(labels)
	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

// formatLabels 格式化 Prometheus 标签（label key 排序，输出稳定）
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := sortedKeys(labels)
	parts := make([]string, 0, len(labels))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatLabelsWithLe 格式化标签并合并 le 标签（le 必须在花括号内才是合法 exposition 格式）
func formatLabelsWithLe(labels map[string]string, le string) string {
	all := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		all[k] = v
	}
	all["le"] = le
	return formatLabels(all)
}

// sortedKeys 返回排序后的 label key
func sortedKeys(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Handler Prometheus /metrics 端点
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 锁内取快照，锁外渲染写出，避免 ReadMemStats / w.Write 长时间持锁
		snap := m.snapshot()

		var sb strings.Builder

		// Go 运行时指标
		sb.WriteString("# HELP go_goroutines Number of goroutines\n")
		sb.WriteString("# TYPE go_goroutines gauge\n")
		sb.WriteString(fmt.Sprintf("go_goroutines %d\n", runtime.NumGoroutine()))

		sb.WriteString("# HELP go_mem_alloc_bytes Number of bytes allocated\n")
		sb.WriteString("# TYPE go_mem_alloc_bytes gauge\n")
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		sb.WriteString(fmt.Sprintf("go_mem_alloc_bytes %d\n", ms.Alloc))
		sb.WriteString(fmt.Sprintf("go_mem_sys_bytes %d\n", ms.Sys))

		// 运行时间
		sb.WriteString("# HELP process_uptime_seconds Uptime in seconds\n")
		sb.WriteString("# TYPE process_uptime_seconds gauge\n")
		sb.WriteString(fmt.Sprintf("process_uptime_seconds %.0f\n", time.Since(m.startTime).Seconds()))

		// 自定义计数器
		sb.WriteString("# HELP cloud_clip_requests_total Total HTTP requests\n")
		sb.WriteString("# TYPE cloud_clip_requests_total counter\n")
		for _, c := range snap.counters {
			sb.WriteString(fmt.Sprintf("cloud_clip_requests_total%s %g\n", formatLabels(c.labels), c.value))
		}

		// 自定义仪表
		sb.WriteString("# HELP cloud_clip_gauge Current gauge values\n")
		sb.WriteString("# TYPE cloud_clip_gauge gauge\n")
		for _, g := range snap.gauges {
			sb.WriteString(fmt.Sprintf("cloud_clip_gauge%s %g\n", formatLabels(g.labels), g.value))
		}

		// 直方图
		sb.WriteString("# HELP cloud_clip_request_duration_seconds Request duration\n")
		sb.WriteString("# TYPE cloud_clip_request_duration_seconds histogram\n")
		for _, h := range snap.histograms {
			for i, b := range h.buckets {
				sb.WriteString(fmt.Sprintf("cloud_clip_request_duration_seconds_bucket%s %d\n",
					formatLabelsWithLe(h.labels, fmt.Sprintf("%g", b)), h.counts[i]))
			}
			sb.WriteString(fmt.Sprintf("cloud_clip_request_duration_seconds_bucket%s %d\n",
				formatLabelsWithLe(h.labels, "+Inf"), h.count))
			sb.WriteString(fmt.Sprintf("cloud_clip_request_duration_seconds_sum%s %g\n",
				formatLabels(h.labels), h.sum))
			sb.WriteString(fmt.Sprintf("cloud_clip_request_duration_seconds_count%s %d\n",
				formatLabels(h.labels), h.count))
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sb.String()))
	}
}

// snapshot 指标快照（锁内拷贝，锁外使用）
type snapshot struct {
	counters   []Counter
	gauges     []Gauge
	histograms []Histogram
}

// snapshot 拷贝当前所有指标数据
func (m *Metrics) snapshot() snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := snapshot{
		counters:   make([]Counter, 0, len(m.counters)),
		gauges:     make([]Gauge, 0, len(m.gauges)),
		histograms: make([]Histogram, 0, len(m.histograms)),
	}
	for _, c := range m.counters {
		snap.counters = append(snap.counters, Counter{value: c.value, labels: copyLabels(c.labels)})
	}
	for _, g := range m.gauges {
		snap.gauges = append(snap.gauges, Gauge{value: g.value, labels: copyLabels(g.labels)})
	}
	for _, h := range m.histograms {
		counts := make([]uint64, len(h.counts))
		copy(counts, h.counts)
		snap.histograms = append(snap.histograms, Histogram{
			buckets: h.buckets,
			counts:  counts,
			sum:     h.sum,
			count:   h.count,
			labels:  copyLabels(h.labels),
		})
	}
	return snap
}

// copyLabels 拷贝 label map，避免调用方后续修改影响快照
func copyLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	cp := make(map[string]string, len(labels))
	for k, v := range labels {
		cp[k] = v
	}
	return cp
}
