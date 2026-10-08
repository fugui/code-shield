package invoker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"code-shield/models"
)

const (
	maxPromptBytes        = 2 << 20 // 2MB
	maxInputFileRefs      = 100
	maxOutputPreviewBytes = 1 << 20 // safety ceiling; callers normally use 64KB
	consoleLineMaxBytes   = 8 << 10
	consoleMaxEvents      = 500
	consoleMaxBytes       = 256 << 10
)

type CallStatus string

const (
	CallRunning   CallStatus = "running"
	CallSucceeded CallStatus = "succeeded"
	CallFailed    CallStatus = "failed"
	CallCanceled  CallStatus = "canceled"
)

// ErrPromptProviderExpired indicates that the live call has completed and the
// bounded prompt provider was intentionally released to avoid retaining req.
var ErrPromptProviderExpired = errors.New("prompt provider expired after call completion")

type InputFileRef struct {
	Path       string `json:"path"`
	DeclaredBy string `json:"declared_by,omitempty"`
}

type PromptSnapshot struct {
	SystemPromptFile    string         `json:"system_prompt_file,omitempty"`
	PromptAvailable     bool           `json:"prompt_available"`
	ResponseFormat      string         `json:"response_format,omitempty"`
	WorkDir             string         `json:"work_dir,omitempty"`
	Source              string         `json:"source,omitempty"`
	InputFiles          []InputFileRef `json:"input_files,omitempty"`
	InputFilesTruncated bool           `json:"input_files_truncated,omitempty"`
	Provider            PromptProvider `json:"-"`
}

type PromptProvider func() (PromptDetail, error)

type PromptDetail struct {
	SystemPrompt   string         `json:"system_prompt,omitempty"`
	UserPrompt     string         `json:"user_prompt,omitempty"`
	RenderedPrompt string         `json:"rendered_prompt,omitempty"`
	InputFiles     []InputFileRef `json:"input_files,omitempty"`
	Truncations    []string       `json:"truncations,omitempty"`
}

type OutputSnapshot struct {
	OutputPath   string `json:"output_path,omitempty"`
	StdoutPath   string `json:"stdout_path,omitempty"`
	DebugLogPath string `json:"debug_log_path,omitempty"`
	Status       string `json:"status,omitempty"`
}

type OutputDetail struct {
	OutputSnapshot
	Exists     bool   `json:"exists"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
	Preview    string `json:"preview,omitempty"`
	Truncated  bool   `json:"truncated"`
	PathRead   string `json:"path_read,omitempty"`
	Error      string `json:"error,omitempty"`
}

type NativeStreamMetrics struct {
	Attempt        int    `json:"attempt,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	StreamEnabled  bool   `json:"stream_enabled"`
	FirstByteMS    int64  `json:"first_byte_ms,omitempty"`
	IdleMS         int64  `json:"idle_ms,omitempty"`
	OutputBytes    int64  `json:"output_bytes,omitempty"`
	ReasoningBytes int64  `json:"reasoning_bytes,omitempty"`
	CancelReason   string `json:"cancel_reason,omitempty"`
	FallbackMode   string `json:"fallback_mode,omitempty"`
}

type ConsoleEvent struct {
	Seq       int64     `json:"seq"`
	Time      time.Time `json:"time"`
	Level     string    `json:"level"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
	Truncated bool      `json:"truncated"`
}

type ConsoleStatus struct {
	Available     bool  `json:"available"`
	Closed        bool  `json:"closed"`
	TotalEvents   int64 `json:"total_events"`
	OldestSeq     int64 `json:"oldest_seq"`
	LastSeq       int64 `json:"last_seq"`
	DroppedEvents int64 `json:"dropped_events"`
}

type ConsoleSnapshot struct {
	NextSeq int64          `json:"next_seq"`
	LastSeq int64          `json:"last_seq"`
	HasMore bool           `json:"has_more"`
	Oldest  int64          `json:"oldest_seq"`
	Dropped int64          `json:"dropped_events"`
	Closed  bool           `json:"closed"`
	Events  []ConsoleEvent `json:"events"`
}

type ConsoleRing struct {
	mu            sync.Mutex
	events        []ConsoleEvent
	nextSeq       int64
	oldestSeq     int64
	totalEvents   int64
	droppedEvents int64
	totalBytes    int
	maxEvents     int
	maxBytes      int
	maxLineBytes  int
	closed        bool
}

type CallObservability struct {
	mu             sync.Mutex
	LeaseID        string
	Driver         string
	Backend        string
	StartedAt      time.Time
	FinishedAt     *time.Time
	Status         CallStatus
	Error          string
	Degraded       bool
	DegradedReason string
	Prompt         *PromptSnapshot
	Console        *ConsoleRing
	Output         *OutputSnapshot
	NativeMetrics  *NativeStreamMetrics
	CLIDiagnostics *CLIDiagnostics
	consoleWriters map[string]*ConsoleWriter
}

var sensitiveValuePattern = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|secret|access[_-]?token|refresh[_-]?token)\b\s*[:=]\s*[^\s,;&}"]+`)
var bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)

func redactSensitiveText(text string) string {
	text = sensitiveValuePattern.ReplaceAllString(text, "$1=[REDACTED]")
	return bearerPattern.ReplaceAllString(text, "Bearer [REDACTED]")
}

func truncateUTF8(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := s[:max]
	for len(cut) > 0 && !isUTF8BoundaryStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut, true
}

func isUTF8BoundaryStart(b byte) bool {
	return b < 0x80 || b >= 0xC0
}

func NewConsoleRing() *ConsoleRing {
	return &ConsoleRing{
		events:       make([]ConsoleEvent, 0, 32),
		maxEvents:    consoleMaxEvents,
		maxBytes:     consoleMaxBytes,
		maxLineBytes: consoleLineMaxBytes,
	}
}

func (r *ConsoleRing) Append(level, stream, format string, args ...any) {
	if r == nil {
		return
	}
	message := format
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	}
	message = redactSensitiveText(message)
	message, truncated := truncateUTF8(message, r.maxLineBytes)
	if truncated {
		message += " ...[truncated]"
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.nextSeq == 0 {
		r.nextSeq = 1
		r.oldestSeq = 1
	}
	event := ConsoleEvent{
		Seq:       r.nextSeq,
		Time:      time.Now(),
		Level:     level,
		Stream:    stream,
		Message:   message,
		Truncated: truncated,
	}
	r.nextSeq++
	r.totalEvents++
	r.events = append(r.events, event)
	r.totalBytes += len(event.Message)
	r.evictLocked()
}

func (r *ConsoleRing) evictLocked() {
	for len(r.events) > r.maxEvents || r.totalBytes > r.maxBytes {
		if len(r.events) == 0 {
			return
		}
		old := r.events[0]
		r.events = r.events[1:]
		r.totalBytes -= len(old.Message)
		r.droppedEvents++
		if len(r.events) > 0 {
			r.oldestSeq = r.events[0].Seq
		}
	}
}

func (r *ConsoleRing) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

func (r *ConsoleRing) Status() ConsoleStatus {
	if r == nil {
		return ConsoleStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

func (r *ConsoleRing) statusLocked() ConsoleStatus {
	status := ConsoleStatus{
		Available:     true,
		Closed:        r.closed,
		TotalEvents:   r.totalEvents,
		OldestSeq:     r.oldestSeq,
		LastSeq:       r.nextSeq - 1,
		DroppedEvents: r.droppedEvents,
	}
	if r.totalEvents == 0 {
		status.OldestSeq = 0
		status.LastSeq = 0
	}
	return status
}

func (r *ConsoleRing) Snapshot(afterSeq int64, limit int) (ConsoleSnapshot, ConsoleStatus) {
	if r == nil {
		return ConsoleSnapshot{Events: []ConsoleEvent{}}, ConsoleStatus{}
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.statusLocked()
	snapshot := ConsoleSnapshot{
		Oldest:  status.OldestSeq,
		Dropped: status.DroppedEvents,
		Closed:  status.Closed,
		Events:  make([]ConsoleEvent, 0, limit),
	}
	for _, event := range r.events {
		if event.Seq <= afterSeq {
			continue
		}
		if len(snapshot.Events) >= limit {
			snapshot.HasMore = true
			break
		}
		snapshot.Events = append(snapshot.Events, event)
	}
	if len(snapshot.Events) > 0 {
		snapshot.NextSeq = snapshot.Events[len(snapshot.Events)-1].Seq
	} else {
		snapshot.NextSeq = afterSeq
	}
	snapshot.LastSeq = status.LastSeq
	return snapshot, status
}

type ConsoleWriter struct {
	ring      *ConsoleRing
	level     string
	stream    string
	partial   []byte
	partialMu sync.Mutex
}

func (r *ConsoleRing) Writer(level, stream string) *ConsoleWriter {
	if r == nil {
		return nil
	}
	return &ConsoleWriter{ring: r, level: level, stream: stream}
}

func (w *ConsoleWriter) Write(p []byte) (int, error) {
	if w == nil || w.ring == nil || len(p) == 0 {
		return len(p), nil
	}
	w.partialMu.Lock()
	defer w.partialMu.Unlock()

	text := strings.ReplaceAll(string(p), "\r", "\n")
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines)-1; i++ {
		line := lines[i]
		if i == 0 && len(w.partial) > 0 {
			line = string(w.partial) + line
			w.partial = nil
		}
		w.ring.Append(w.level, w.stream, "%s", line)
	}

	if last := lines[len(lines)-1]; last != "" {
		w.partial = append(w.partial, last...)
		if len(w.partial) > w.ring.maxLineBytes {
			w.flushPartialLocked()
		}
	} else if len(lines) > 1 && len(w.partial) == 0 {
		// A trailing newline was handled by the loop above.
	}
	return len(p), nil
}

func (w *ConsoleWriter) Flush() {
	if w == nil {
		return
	}
	w.partialMu.Lock()
	defer w.partialMu.Unlock()
	w.flushPartialLocked()
}

func (w *ConsoleWriter) flushPartialLocked() {
	if len(w.partial) == 0 {
		return
	}
	w.ring.Append(w.level, w.stream, "%s", string(w.partial))
	w.partial = nil
}

func NewCallObservability(req AIRequest, backend, driver string) *CallObservability {
	now := time.Now()
	obs := &CallObservability{
		Backend:   backend,
		Driver:    driver,
		StartedAt: now,
		Status:    CallRunning,
		Console:   NewConsoleRing(),
		Output: &OutputSnapshot{
			OutputPath:   req.OutputPath,
			StdoutPath:   req.OutputPath + ".output.txt",
			DebugLogPath: req.OutputPath + ".debug.log",
		},
	}
	obs.Prompt = newPromptSnapshot(req, driver)
	obs.consoleWriters = map[string]*ConsoleWriter{
		"stdout": obs.Console.Writer("stdout", "stdout"),
		"stderr": obs.Console.Writer("stderr", "stderr"),
	}
	return obs
}

func (o *CallObservability) ConsoleWriter(stream string) *ConsoleWriter {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.consoleWriters[stream]
}

func (o *CallObservability) FlushConsole() {
	if o == nil {
		return
	}
	o.mu.Lock()
	writers := make([]*ConsoleWriter, 0, len(o.consoleWriters))
	for _, writer := range o.consoleWriters {
		writers = append(writers, writer)
	}
	o.mu.Unlock()
	for _, writer := range writers {
		writer.Flush()
	}
}

func (o *CallObservability) MarkDegraded(reason string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Degraded = true
	if o.DegradedReason == "" {
		o.DegradedReason = reason
	}
}

func (o *CallObservability) SetNativeMetrics(metrics NativeStreamMetrics) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	copied := metrics
	o.NativeMetrics = &copied
}

func (o *CallObservability) NativeMetricsSnapshot() *NativeStreamMetrics {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.NativeMetrics == nil {
		return nil
	}
	copied := *o.NativeMetrics
	return &copied
}

func (o *CallObservability) SetCLIDiagnostics(diag CLIDiagnostics) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	diag.LeaseID = o.LeaseID
	copied := diag
	o.CLIDiagnostics = &copied
}

func (o *CallObservability) CLIDiagnosticsSnapshot() *CLIDiagnostics {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CLIDiagnostics == nil {
		return nil
	}
	copied := *o.CLIDiagnostics
	return &copied
}

func (o *CallObservability) MergeCLIDiagnostics(merge func(*CLIDiagnostics)) {
	if o == nil || merge == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	diag := CLIDiagnostics{Driver: o.Driver}
	if o.CLIDiagnostics != nil {
		diag = *o.CLIDiagnostics
	}
	merge(&diag)
	o.CLIDiagnostics = &diag
}

func (o *CallObservability) Finish(err error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.FinishedAt != nil {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()

	now := time.Now()
	status := CallSucceeded
	message := ""
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled):
		status = CallCanceled
		message = err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status = CallFailed
		message = "context deadline exceeded"
	default:
		status = CallFailed
		message = err.Error()
	}
	if len(message) > 2048 {
		message = message[:2048] + " ...[truncated]"
	}

	o.mu.Lock()
	o.FinishedAt = &now
	o.Status = status
	o.Error = message
	if o.Prompt != nil {
		o.Prompt.Provider = nil
	}
	o.mu.Unlock()

	if o.Console != nil {
		o.Console.Append("system", "system", "lease %s", string(status))
		o.Console.Close()
	}
}

func (o *CallObservability) PromptDetail() (PromptDetail, error) {
	if o == nil || o.Prompt == nil || o.Prompt.Provider == nil {
		return PromptDetail{}, ErrPromptProviderExpired
	}
	o.mu.Lock()
	provider := o.Prompt.Provider
	o.mu.Unlock()
	return provider()
}

func (o *CallObservability) ReleasePromptProvider() {
	if o == nil || o.Prompt == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Prompt.Provider = nil
}

func (o *CallObservability) PromptAvailable() bool {
	if o == nil || o.Prompt == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.Prompt.PromptAvailable && o.Prompt.Provider != nil
}

func (o *CallObservability) OutputDetail(maxBytes int) OutputDetail {
	if o == nil || o.Output == nil {
		return OutputDetail{}
	}
	if maxBytes <= 0 || maxBytes > maxOutputPreviewBytes {
		maxBytes = 64 << 10
	}

	o.mu.Lock()
	output := *o.Output
	o.mu.Unlock()

	detail := OutputDetail{OutputSnapshot: output}
	candidates := []string{output.OutputPath, output.StdoutPath, output.DebugLogPath}
	var staleDetail *OutputDetail
	for _, path := range candidates {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			detail.Error = "diagnostic output is not a regular file"
			continue
		}
		modifiedAt := info.ModTime()
		stale := !o.StartedAt.IsZero() && modifiedAt.Before(o.StartedAt)
		file, err := os.Open(path)
		if err != nil {
			detail.Error = err.Error()
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
		_ = file.Close()
		if readErr != nil {
			detail.Error = readErr.Error()
			continue
		}
		truncated := false
		if len(content) > maxBytes {
			content = content[:maxBytes]
			truncated = true
		}
		detail.Exists = true
		detail.SizeBytes = info.Size()
		detail.ModifiedAt = modifiedAt.Format(time.RFC3339Nano)
		detail.Stale = stale
		detail.PathRead = path
		detail.Preview = string(content)
		detail.Truncated = truncated
		if stale {
			if staleDetail == nil {
				staleDetail = &detail
			}
			continue
		}
		return detail
	}
	if staleDetail != nil {
		return *staleDetail
	}
	return detail
}

func newPromptSnapshot(req AIRequest, driver string) *PromptSnapshot {
	inputFiles := make([]InputFileRef, 0, minInt(len(req.InputFiles), maxInputFileRefs))
	for _, path := range req.InputFiles {
		if len(inputFiles) >= maxInputFileRefs {
			break
		}
		inputFiles = append(inputFiles, InputFileRef{Path: path, DeclaredBy: driver})
	}
	return &PromptSnapshot{
		SystemPromptFile:    req.PromptFile,
		PromptAvailable:     req.PromptFile != "" || req.PromptMsg != "",
		ResponseFormat:      req.ResponseFormat,
		WorkDir:             req.WorkDir,
		Source:              "declared/best-effort",
		InputFiles:          inputFiles,
		InputFilesTruncated: len(req.InputFiles) > maxInputFileRefs,
		Provider:            newPromptProvider(req, driver),
	}
}

func newPromptProvider(req AIRequest, driver string) PromptProvider {
	return func() (PromptDetail, error) {
		detail := PromptDetail{InputFiles: newPromptSnapshot(req, driver).InputFiles}
		var truncations []string

		if req.PromptFile != "" {
			promptPath := req.PromptFile
			if driver == "native" {
				// Match the real NativeInvoker path resolution exactly.
				promptPath = models.AppConfig.GetAbsPath(req.PromptFile)
			}
			content, truncated, err := readBoundedTextFile(promptPath, maxPromptBytes)
			if err != nil {
				return detail, fmt.Errorf("failed to read prompt file: %w", err)
			}
			if truncated {
				truncations = append(truncations, "system prompt truncated to 2MB")
			}
			switch driver {
			case "native", "claude":
				detail.SystemPrompt = content
			default:
				// CLI drivers that inline the template will expose it through
				// RenderedPrompt below; keep the template separate for clarity.
				detail.SystemPrompt = content
			}
		}

		switch driver {
		case "native":
			detail.UserPrompt = req.PromptMsg
		case "opencode", "codex", "agy":
			rendered, err := BuildPromptPayload(req, true)
			if err != nil {
				return detail, err
			}
			rendered, truncated := truncateUTF8(rendered, maxPromptBytes)
			if truncated {
				truncations = append(truncations, "rendered prompt truncated to 2MB")
			}
			detail.RenderedPrompt = rendered
		case "claude":
			rendered, err := BuildPromptPayload(req, false)
			if err != nil {
				return detail, err
			}
			rendered, truncated := truncateUTF8(rendered, maxPromptBytes)
			if truncated {
				truncations = append(truncations, "user prompt truncated to 2MB")
			}
			detail.UserPrompt = rendered
		default:
			detail.UserPrompt = req.PromptMsg
		}

		detail.Truncations = truncations
		return detail, nil
	}
}

func readBoundedTextFile(path string, maxBytes int) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("prompt file is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return "", false, err
	}
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncatedText, _ := truncateUTF8(string(content), maxBytes)
		return truncatedText, true, nil
	}
	return string(content), false, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
