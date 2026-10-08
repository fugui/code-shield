package dispatcher

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"code-shield/services/invoker"
)

const (
	passThroughPoolKey = "passthrough"
	systemTierName     = "system_tool"
	maxErrorMessage    = 512
)

var (
	secretQueryPattern  = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|secret|token)=([^&\s'"]+)`)
	secretHeaderPattern = regexp.MustCompile(`(?i)(authorization|proxy-authorization|x-api-key)\s*:\s*(?:bearer\s+)?[^\s,;]+`)
	bearerTokenPattern  = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`)
)

var durationBuckets = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	3 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	time.Hour,
}

type tierPoolKey struct {
	TierName string
	PoolKey  string
}

type dispatcherMetrics struct {
	startedAt time.Time
	pools     map[string]*MetricSeries
	tiers     map[string]*TierMetricSeries
	tierPools map[tierPoolKey]*MetricSeries
}

// MetricSeries 记录同一个统计维度下的模型调用质量。它只保存有界计数和
// 固定耗时桶，不保存请求、Prompt、输出或源码内容。
type MetricSeries struct {
	Assigned  uint64
	Completed uint64
	Succeeded uint64
	Failed    uint64
	Canceled  uint64
	Timeout   uint64

	ExecutionDurationNanos        uint64
	SuccessExecutionDurationNanos uint64
	ExecutionHistogram            []uint64

	LastCompletedAt *time.Time
	LastError       string
	LastErrorAt     *time.Time
}

// TierMetricSeries 在调用质量之外补充调度等待和 passthrough 计数。
type TierMetricSeries struct {
	MetricSeries

	Dispatched      uint64
	SlotAssigned    uint64
	Passthrough     uint64
	AcquireFailed   uint64
	AcquireCanceled uint64
	AcquireTimeout  uint64

	QueueDurationNanos uint64
	QueueHistogram     []uint64
}

// MetricSnapshot is the JSON-facing projection of MetricSeries.
type MetricSnapshot struct {
	Assigned  uint64 `json:"assigned"`
	Completed uint64 `json:"completed"`
	Succeeded uint64 `json:"succeeded"`
	Failed    uint64 `json:"failed"`
	Canceled  uint64 `json:"canceled"`
	Timeout   uint64 `json:"timeout"`

	AvgExecutionSeconds        float64 `json:"avg_execution_seconds"`
	AvgSuccessExecutionSeconds float64 `json:"avg_success_execution_seconds"`
	P95ExecutionSeconds        float64 `json:"p95_execution_seconds"`

	LastCompletedAt *time.Time `json:"last_completed_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastErrorAt     *time.Time `json:"last_error_at,omitempty"`
}

// PoolMetricsSnapshot combines one pool's aggregate counters with live slot state.
type PoolMetricsSnapshot struct {
	PoolKey       string         `json:"pool_key"`
	DisplayName   string         `json:"display_name"`
	ResourceID    string         `json:"resource_id,omitempty"`
	Driver        string         `json:"driver,omitempty"`
	Model         string         `json:"model,omitempty"`
	Running       int            `json:"running"`
	Limit         int            `json:"limit"`
	RawConcurrent int            `json:"raw_concurrent"`
	Metrics       MetricSnapshot `json:"metrics"`
}

// TierMetricsSnapshot is one logical tier or role's quality view.
type TierMetricsSnapshot struct {
	TierName        string                    `json:"tier_name"`
	Level           int                       `json:"level"`
	Role            string                    `json:"role"`
	Running         uint64                    `json:"running"`
	Dispatched      uint64                    `json:"dispatched"`
	SlotAssigned    uint64                    `json:"slot_assigned"`
	Passthrough     uint64                    `json:"passthrough"`
	AcquireFailed   uint64                    `json:"acquire_failed"`
	AcquireCanceled uint64                    `json:"acquire_canceled"`
	AcquireTimeout  uint64                    `json:"acquire_timeout"`
	AvgQueueSeconds float64                   `json:"avg_queue_seconds"`
	P95QueueSeconds float64                   `json:"p95_queue_seconds"`
	Metrics         MetricSnapshot            `json:"metrics"`
	ByPool          []TierPoolMetricsSnapshot `json:"by_pool,omitempty"`
}

// TierPoolMetricsSnapshot exposes Tier x Pool fallback and affinity traffic.
type TierPoolMetricsSnapshot struct {
	PoolKey string         `json:"pool_key"`
	Metrics MetricSnapshot `json:"metrics"`
}

// DispatcherMetricsSnapshot is a consistent process-lifetime view.
type DispatcherMetricsSnapshot struct {
	Scope       string                `json:"scope"`
	Since       time.Time             `json:"since"`
	GeneratedAt time.Time             `json:"generated_at"`
	Pools       []PoolMetricsSnapshot `json:"pools"`
	Tiers       []TierMetricsSnapshot `json:"tiers"`
}

// DispatcherDebugSnapshot groups the read-only views used by the system debug
// dashboard so resource slots, leases, and metrics come from one lock window.
type DispatcherDebugSnapshot struct {
	Resources              []ModelResourceStatus     `json:"resources"`
	ActiveLeases           []LLMSlotLease            `json:"active_leases"`
	RecentLeases           []LLMSlotLease            `json:"recent_leases"`
	RecordSuccessfulLeases bool                      `json:"record_successful_leases"`
	Metrics                DispatcherMetricsSnapshot `json:"metrics"`
}

func newDispatcherMetrics() *dispatcherMetrics {
	m := &dispatcherMetrics{
		startedAt: time.Now(),
		pools:     make(map[string]*MetricSeries),
		tiers:     make(map[string]*TierMetricSeries),
		tierPools: make(map[tierPoolKey]*MetricSeries),
	}
	m.ensureMaps()
	return m
}

func (m *dispatcherMetrics) ensureMaps() {
	if m.startedAt.IsZero() {
		m.startedAt = time.Now()
	}
	if m.pools == nil {
		m.pools = make(map[string]*MetricSeries)
	}
	if m.tiers == nil {
		m.tiers = make(map[string]*TierMetricSeries)
	}
	if m.tierPools == nil {
		m.tierPools = make(map[tierPoolKey]*MetricSeries)
	}
}

func (m *dispatcherMetrics) poolSeries(key string) *MetricSeries {
	if m.pools == nil {
		m.pools = make(map[string]*MetricSeries)
	}
	series, ok := m.pools[key]
	if !ok {
		series = newMetricSeries()
		m.pools[key] = series
	}
	return series
}

func (m *dispatcherMetrics) tierSeries(name string) *TierMetricSeries {
	if m.tiers == nil {
		m.tiers = make(map[string]*TierMetricSeries)
	}
	key := CanonicalTierName(name)
	series, ok := m.tiers[key]
	if !ok {
		series = newTierMetricSeries()
		m.tiers[key] = series
	}
	return series
}

func (m *dispatcherMetrics) tierPoolSeries(tierName, poolKey string) *MetricSeries {
	if m.tierPools == nil {
		m.tierPools = make(map[tierPoolKey]*MetricSeries)
	}
	key := tierPoolKey{TierName: CanonicalTierName(tierName), PoolKey: poolKey}
	series, ok := m.tierPools[key]
	if !ok {
		series = newMetricSeries()
		m.tierPools[key] = series
	}
	return series
}

func newMetricSeries() *MetricSeries {
	return &MetricSeries{ExecutionHistogram: make([]uint64, len(durationBuckets)+1)}
}

func newTierMetricSeries() *TierMetricSeries {
	return &TierMetricSeries{
		MetricSeries:   MetricSeries{ExecutionHistogram: make([]uint64, len(durationBuckets)+1)},
		QueueHistogram: make([]uint64, len(durationBuckets)+1),
	}
}

func (s *MetricSeries) addExecution(duration time.Duration, success bool) {
	if duration < 0 {
		duration = 0
	}
	s.Completed++
	s.ExecutionDurationNanos += uint64(duration.Nanoseconds())
	addToHistogram(s.ExecutionHistogram, duration)
	if success {
		s.Succeeded++
		s.SuccessExecutionDurationNanos += uint64(duration.Nanoseconds())
	}
	now := time.Now()
	s.LastCompletedAt = &now
}

func (s *MetricSeries) addResult(err error) {
	switch {
	case err == nil:
		// Status and duration are set by addExecution.
	case errors.Is(err, context.Canceled):
		s.Canceled++
	case errors.Is(err, context.DeadlineExceeded):
		s.Failed++
		s.Timeout++
	default:
		s.Failed++
	}
	if err != nil {
		now := time.Now()
		s.LastError = SanitizeMetricError(err)
		s.LastErrorAt = &now
	}
}

func (s *TierMetricSeries) addQueue(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	s.QueueDurationNanos += uint64(duration.Nanoseconds())
	addToHistogram(s.QueueHistogram, duration)
}

func addToHistogram(histogram []uint64, duration time.Duration) {
	if len(histogram) == 0 {
		return
	}
	for i, bound := range durationBuckets {
		if duration <= bound {
			histogram[i]++
			return
		}
	}
	histogram[len(histogram)-1]++
}

func percentileSeconds(histogram []uint64, total uint64, percentile float64) float64 {
	if total == 0 || len(histogram) == 0 {
		return 0
	}
	target := uint64(float64(total)*percentile + 0.999999)
	if target < 1 {
		target = 1
	}
	var seen uint64
	for i, count := range histogram {
		seen += count
		if seen >= target {
			if i < len(durationBuckets) {
				return durationBuckets[i].Seconds()
			}
			return durationBuckets[len(durationBuckets)-1].Seconds()
		}
	}
	return 0
}

// CanonicalTierName normalizes compatibility names and prevents raw stage text
// from becoming a statistics key.
func CanonicalTierName(name string) string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "tier1_hunter":
		return "tier1_hunter"
	case "tier2_challenger":
		return "tier2_challenger"
	case "tier3_judge":
		return "tier3_judge"
	case "tier4_synthesis":
		return "tier4_synthesis"
	case "":
		return systemTierName
	default:
		return strings.TrimSpace(strings.ToLower(name))
	}
}

func tierDisplay(name string) (int, string) {
	switch CanonicalTierName(name) {
	case "tier1_hunter":
		return 1, "Hunter"
	case "tier2_challenger":
		return 2, "Challenger"
	case "tier3_judge":
		return 3, "Judge"
	case "tier4_synthesis":
		return 4, "Synthesis"
	case systemTierName:
		return 0, "System Tool"
	default:
		return 0, "Uncategorized"
	}
}

func metricPoolKey(res *ModelResource) string {
	if res == nil {
		return passThroughPoolKey
	}
	return res.ResourceKey()
}

func effectiveModelName(res *ModelResource, backend string) string {
	if res == nil {
		return ""
	}
	if model := res.ModelName(backend); model != "" {
		return model
	}
	if res.Model != "" {
		return res.Model
	}
	for _, model := range []string{res.OpenCode, res.Claude, res.Codex, res.Agy, res.Native} {
		if model != "" {
			return model
		}
	}
	return ""
}

func SanitizeMetricError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	message = redactMetricSecrets(message)
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > maxErrorMessage {
		limit := maxErrorMessage
		for limit > 0 && !utf8.RuneStart(message[limit]) {
			limit--
		}
		return message[:limit] + "..."
	}
	return message
}

func redactMetricSecrets(message string) string {
	redacted := secretQueryPattern.ReplaceAllString(message, "$1=[REDACTED]")
	redacted = secretHeaderPattern.ReplaceAllString(redacted, "$1: [REDACTED]")
	redacted = bearerTokenPattern.ReplaceAllString(redacted, "Bearer [REDACTED]")
	return redacted
}

func resolveTierName(req invoker.AIRequest, workCtx *invoker.LLMWorkContext) string {
	if req.TierName != "" {
		return req.TierName
	}
	if workCtx != nil && workCtx.TierName != "" {
		return workCtx.TierName
	}
	return systemTierName
}

func classifyMetricError(err error) (bool, bool) {
	return errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded)
}

func (s MetricSeries) snapshot() MetricSnapshot {
	avg := float64(0)
	if s.Completed > 0 {
		avg = float64(s.ExecutionDurationNanos) / float64(s.Completed) / float64(time.Second)
	}
	avgSuccess := float64(0)
	if s.Succeeded > 0 {
		avgSuccess = float64(s.SuccessExecutionDurationNanos) / float64(s.Succeeded) / float64(time.Second)
	}
	return MetricSnapshot{
		Assigned:                   s.Assigned,
		Completed:                  s.Completed,
		Succeeded:                  s.Succeeded,
		Failed:                     s.Failed,
		Canceled:                   s.Canceled,
		Timeout:                    s.Timeout,
		AvgExecutionSeconds:        avg,
		AvgSuccessExecutionSeconds: avgSuccess,
		P95ExecutionSeconds:        percentileSeconds(s.ExecutionHistogram, s.Completed, 0.95),
		LastCompletedAt:            s.LastCompletedAt,
		LastError:                  s.LastError,
		LastErrorAt:                s.LastErrorAt,
	}
}

func (s TierMetricSeries) snapshot() (MetricSnapshot, float64, float64) {
	avgQueue := float64(0)
	if s.SlotAssigned > 0 {
		avgQueue = float64(s.QueueDurationNanos) / float64(s.SlotAssigned) / float64(time.Second)
	}
	return s.MetricSeries.snapshot(), avgQueue, percentileSeconds(s.QueueHistogram, s.SlotAssigned, 0.95)
}

// GetMetricsSnapshot returns pool, tier, and Tier x Pool aggregates together
// with the current live slot state. All state is read under the dispatcher lock.
func (d *ModelDispatcher) GetMetricsSnapshot() DispatcherMetricsSnapshot {
	if d == nil {
		return DispatcherMetricsSnapshot{
			Scope:       "process_lifetime",
			Since:       time.Now(),
			GeneratedAt: time.Now(),
			Pools:       []PoolMetricsSnapshot{},
			Tiers:       []TierMetricsSnapshot{},
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getMetricsSnapshotLocked(time.Now())
}

// GetDebugSnapshot returns one consistent snapshot for the debug overview API.
func (d *ModelDispatcher) GetDebugSnapshot() DispatcherDebugSnapshot {
	if d == nil {
		now := time.Now()
		return DispatcherDebugSnapshot{
			ActiveLeases:           []LLMSlotLease{},
			RecentLeases:           []LLMSlotLease{},
			RecordSuccessfulLeases: false,
			Metrics: DispatcherMetricsSnapshot{
				Scope:       "process_lifetime",
				Since:       now,
				GeneratedAt: now,
				Pools:       []PoolMetricsSnapshot{},
				Tiers:       []TierMetricsSnapshot{},
			},
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	return DispatcherDebugSnapshot{
		Resources:              d.getResourcesStatusLocked(now),
		ActiveLeases:           d.getActiveLeasesLocked(now),
		RecentLeases:           d.getRecentLeasesLocked(),
		RecordSuccessfulLeases: !d.skipSuccessfulLeases,
		Metrics:                d.getMetricsSnapshotLocked(now),
	}
}

func (d *ModelDispatcher) getMetricsSnapshotLocked(now time.Time) DispatcherMetricsSnapshot {
	m := d.metricsLocked()

	seenPools := make(map[string]bool, len(m.pools)+len(d.resources))
	pools := make([]PoolMetricsSnapshot, 0, len(m.pools)+len(d.resources))

	info := d.getEffectiveScaleInfoLocked(now)
	for _, res := range d.resources {
		key := metricPoolKey(res)
		if seenPools[key] {
			continue
		}
		seenPools[key] = true
		series, ok := m.pools[key]
		if !ok {
			series = newMetricSeries()
			m.pools[key] = series
		}
		limit := calculateLimit(res.Concurrent, info.EffectiveScale)
		displayName := res.ID
		if displayName == "" {
			displayName = key
		}
		pools = append(pools, PoolMetricsSnapshot{
			PoolKey:       key,
			DisplayName:   displayName,
			ResourceID:    res.ID,
			Driver:        res.Driver,
			Model:         effectiveModelName(res, res.Driver),
			Running:       res.Active,
			Limit:         limit,
			RawConcurrent: res.Concurrent,
			Metrics:       series.snapshot(),
		})
	}

	for key, series := range m.pools {
		if seenPools[key] {
			continue
		}
		seenPools[key] = true
		driver := ""
		if key == passThroughPoolKey {
			driver = "passthrough"
		}
		pools = append(pools, PoolMetricsSnapshot{
			PoolKey:     key,
			DisplayName: key,
			Driver:      driver,
			Metrics:     series.snapshot(),
		})
	}
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].PoolKey < pools[j].PoolKey
	})

	runningByTier := make(map[string]uint64, len(m.tiers)+1)
	for _, lease := range d.activeLeases {
		runningByTier[CanonicalTierName(lease.TierName)]++
	}

	tiers := make([]TierMetricsSnapshot, 0, len(m.tiers))
	for tierName, series := range m.tiers {
		level, role := tierDisplay(tierName)
		metricSnapshot, avgQueue, p95Queue := series.snapshot()
		tier := TierMetricsSnapshot{
			TierName:        tierName,
			Level:           level,
			Role:            role,
			Running:         runningByTier[tierName],
			Dispatched:      series.Dispatched,
			SlotAssigned:    series.SlotAssigned,
			Passthrough:     series.Passthrough,
			AcquireFailed:   series.AcquireFailed,
			AcquireCanceled: series.AcquireCanceled,
			AcquireTimeout:  series.AcquireTimeout,
			AvgQueueSeconds: avgQueue,
			P95QueueSeconds: p95Queue,
			Metrics:         metricSnapshot,
		}

		for crossKey, crossSeries := range m.tierPools {
			if crossKey.TierName != tierName {
				continue
			}
			tier.ByPool = append(tier.ByPool, TierPoolMetricsSnapshot{
				PoolKey: crossKey.PoolKey,
				Metrics: crossSeries.snapshot(),
			})
		}
		sort.Slice(tier.ByPool, func(i, j int) bool {
			return tier.ByPool[i].PoolKey < tier.ByPool[j].PoolKey
		})
		tiers = append(tiers, tier)
	}
	sort.Slice(tiers, func(i, j int) bool {
		if tiers[i].Level != tiers[j].Level {
			return tiers[i].Level < tiers[j].Level
		}
		return tiers[i].TierName < tiers[j].TierName
	})

	return DispatcherMetricsSnapshot{
		Scope:       "process_lifetime",
		Since:       m.startedAt,
		GeneratedAt: now,
		Pools:       pools,
		Tiers:       tiers,
	}
}

func (d *ModelDispatcher) metricsLocked() *dispatcherMetrics {
	if d.metrics == nil {
		d.metrics = newDispatcherMetrics()
	}
	d.metrics.ensureMaps()
	return d.metrics
}

// RecordDispatchStarted counts a model invocation entering the dispatcher.
func (d *ModelDispatcher) RecordDispatchStarted(tierName string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.metricsLocked()
	m.tierSeries(tierName).Dispatched++
}

// RecordDispatchAssigned counts a call after a real pool slot was obtained.
func (d *ModelDispatcher) RecordDispatchAssigned(tierName string, res *ModelResource, queueWait time.Duration) {
	if d == nil || res == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	poolKey := metricPoolKey(res)
	m := d.metricsLocked()
	tier := m.tierSeries(tierName)
	tier.SlotAssigned++
	tier.addQueue(queueWait)

	pool := m.poolSeries(poolKey)
	pool.Assigned++

	cross := m.tierPoolSeries(tierName, poolKey)
	cross.Assigned++
}

// RecordDispatchPassthrough counts calls executed outside physical pool control.
func (d *ModelDispatcher) RecordDispatchPassthrough(tierName string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	m := d.metricsLocked()
	m.tierSeries(tierName).Passthrough++
	pool := m.poolSeries(passThroughPoolKey)
	pool.Assigned++
	cross := m.tierPoolSeries(tierName, passThroughPoolKey)
	cross.Assigned++
}

// RecordDispatchAcquireFailure records callers that never obtained a slot.
func (d *ModelDispatcher) RecordDispatchAcquireFailure(tierName string, err error) {
	if d == nil {
		return
	}
	canceled, timeout := classifyMetricError(err)
	d.mu.Lock()
	defer d.mu.Unlock()

	tier := d.metricsLocked().tierSeries(tierName)
	switch {
	case canceled:
		tier.AcquireCanceled++
	case timeout:
		tier.AcquireTimeout++
	default:
		tier.AcquireFailed++
	}
}

// RecordDispatchCompleted records the terminal status and execution duration of
// one model invocation. Pool assignment is not incremented again here.
func (d *ModelDispatcher) RecordDispatchCompleted(tierName string, res *ModelResource, execution time.Duration, invokeErr error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	poolKey := metricPoolKey(res)
	m := d.metricsLocked()
	tier := m.tierSeries(tierName)
	tier.addExecution(execution, invokeErr == nil)
	tier.addResult(invokeErr)

	pool := m.poolSeries(poolKey)
	pool.addExecution(execution, invokeErr == nil)
	pool.addResult(invokeErr)

	cross := m.tierPoolSeries(tierName, poolKey)
	cross.addExecution(execution, invokeErr == nil)
	cross.addResult(invokeErr)
}
