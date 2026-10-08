package invoker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code-shield/models"
)

// CircuitBreakerState 断路器状态
type CircuitBreakerState int

const (
	StateClosed   CircuitBreakerState = iota // 正常通信
	StateOpen                                // 熔断开启（请求降级至 CLI）
	StateHalfOpen                            // 半开探测
)

const (
	defaultNativeTextMaxTokens = 32768
	nativeUserAgent            = "CodeShield/1.0.0"
)

// NativeInvoker 采用原生 HTTP REST API 直连 LLM 服务，无外部 OS 进程依赖
type NativeInvoker struct {
	client *http.Client

	// 熔断与降级状态机
	mu                  sync.Mutex
	cbState             CircuitBreakerState
	consecutiveFailures int
	lastFailureTime     time.Time
	failureThreshold    int           // 连续失败多少次触发熔断 (默认 3)
	recoveryTimeout     time.Duration // 熔断后冷却多久尝试半开恢复 (默认 30s)
}

// NewNativeInvoker 创建 NativeInvoker 实例
func NewNativeInvoker() *NativeInvoker {
	return &NativeInvoker{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 60 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: 15 * time.Second,
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     120 * time.Second,
				DisableKeepAlives:   true,
			},
		},
		cbState:          StateClosed,
		failureThreshold: 3,
		recoveryTimeout:  30 * time.Second,
	}
}

func (n *NativeInvoker) Name() string {
	return "native"
}

// resolveEndpoints 智能解析当前可用的异构端点候选列表
func (n *NativeInvoker) resolveEndpoints(targetModel string) []models.NativeEndpointConfig {
	cfg := models.AppConfig.AI.Native
	if len(cfg.Endpoints) == 0 {
		return nil
	}

	var matched []models.NativeEndpointConfig
	if targetModel != "" {
		for _, ep := range cfg.Endpoints {
			if strings.EqualFold(ep.Model, targetModel) {
				matched = append(matched, ep)
			}
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return cfg.Endpoints
}

// checkCircuitBreaker 检查断路器状态，若处于熔断且在冷却期内返回 true（表示需降级）
func (n *NativeInvoker) checkCircuitBreaker() bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cbState == StateOpen {
		if time.Since(n.lastFailureTime) > n.recoveryTimeout {
			n.cbState = StateHalfOpen
			log.Println("[NativeInvoker] Circuit breaker switched from OPEN to HALF-OPEN (probing...)")
			return false
		}
		return true
	}
	return false
}

// recordSuccess 记录一次成功的调用，重置熔断计数
func (n *NativeInvoker) recordSuccess() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cbState == StateHalfOpen || n.consecutiveFailures > 0 {
		log.Printf("[NativeInvoker] Circuit breaker restored to CLOSED (consecutive failures reset from %d to 0)\n", n.consecutiveFailures)
	}
	n.cbState = StateClosed
	n.consecutiveFailures = 0
}

// recordFailure 记录一次失败的调用，并在达到阈值时开启熔断
func (n *NativeInvoker) recordFailure() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.consecutiveFailures++
	n.lastFailureTime = time.Now()
	if n.consecutiveFailures >= n.failureThreshold && n.cbState != StateOpen {
		n.cbState = StateOpen
		log.Printf("[NativeInvoker] Circuit breaker TRIPPED to OPEN after %d consecutive failures. Future requests will temporarily fallback to CLI.\n", n.consecutiveFailures)
	}
}

// fallbackToCLI 当 Native 引擎熔断或不可用时，平滑降级到配置中的 Thick Agent。
// AI.Backend 可能本身是 native，因此不能直接把它当作 fallback；必须显式发现非 native CLI 驱动。
// selectNonNativeFallbackResource returns the first configured and registered
// CLI backend together with its owning resource.  Keeping the resource is
// necessary because a native model alias (for example "dp") is frequently not
// a valid CLI model identifier (for example "modelgate/dp").
func selectNonNativeFallbackResource() (string, *models.ComputeResourceConfig) {
	for i := range models.AppConfig.LLM.Resources {
		resource := &models.AppConfig.LLM.Resources[i]
		driver := strings.TrimSpace(resource.Driver)
		if driver == "" || strings.EqualFold(driver, "native") {
			continue
		}
		if inv, ok := GetRawInvoker(driver); ok && inv != nil {
			return driver, resource
		}
	}

	// Compatibility with legacy AI.Models configs.
	legacy := models.AppConfig.AI.Backend
	if !strings.EqualFold(legacy, "native") {
		if inv, ok := GetRawInvoker(legacy); ok && inv != nil {
			return legacy, nil
		}
	}
	return "", nil
}

func (n *NativeInvoker) fallbackToCLI(req AIRequest) error {
	fallbackBackend, fallbackResource := selectNonNativeFallbackResource()
	if fallbackBackend == "" {
		return fmt.Errorf("circuit breaker fallback failed: no non-native CLI backend is configured or registered")
	}
	log.Printf("[NativeInvoker] Falling back request to CLI backend %q\n", fallbackBackend)
	// Native and CLI drivers do not share the model namespace.  The selected
	// CLI resource is authoritative; otherwise native-only aliases leak into
	// the CLI process and fail before a single token is generated.
	fallbackModel := ""
	if fallbackResource != nil {
		fallbackModel = fallbackResource.ResourceModel()
	}
	if fallbackResource != nil && strings.TrimSpace(fallbackModel) != "" {
		req.ModelName = fallbackModel
	} else if req.ModelName != "" {
		req.ModelName = ""
	}

	inv, ok := GetRawInvoker(fallbackBackend)
	if !ok || inv == nil {
		return fmt.Errorf("circuit breaker fallback failed: CLI backend %q is not registered", fallbackBackend)
	}
	return inv.Invoke(req)
}

func (n *NativeInvoker) Invoke(req AIRequest) error {
	// 1. 检查断路器，若已熔断直接平滑降级至本地 CLI
	if n.checkCircuitBreaker() {
		log.Println("[NativeInvoker] Circuit breaker is OPEN, routing directly to CLI fallback")
		if req.Observability != nil {
			req.Observability.Console.Append("debug", "system", "circuit breaker open; using CLI fallback")
		}
		return n.fallbackToCLI(req)
	}

	ctx := req.ParentContext
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		if req.TimeoutMin > 0 {
			timeout = time.Duration(req.TimeoutMin) * time.Minute
		} else {
			timeout = 10 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cfg := models.AppConfig.AI.Native
	if req.AttemptTimeoutSeconds > 0 {
		cfg.AttemptTimeoutSeconds = req.AttemptTimeoutSeconds
	}
	if req.FirstByteTimeoutSeconds > 0 {
		cfg.FirstByteTimeoutSeconds = req.FirstByteTimeoutSeconds
	}
	if req.IdleTimeoutSeconds > 0 {
		cfg.IdleTimeoutSeconds = req.IdleTimeoutSeconds
	}
	if req.MaxOutputBytes > 0 {
		cfg.MaxOutputBytes = req.MaxOutputBytes
	}
	cleanInvocationArtifacts(req.OutputPath)

	// 2. 拆分 System Prompt 与 User Prompt
	var messages []map[string]string
	if req.PromptFile != "" {
		promptPath := models.AppConfig.GetAbsPath(req.PromptFile)
		if promptBytes, err := os.ReadFile(promptPath); err == nil && len(promptBytes) > 0 {
			messages = append(messages, map[string]string{
				"role":    "system",
				"content": string(promptBytes),
			})
		}
	}

	userContent := req.PromptMsg
	// 若传递了 InputFiles 且 Prompt 中未包含对应文件内容，且文件存在，附带作为上下文
	// 加入 Token 软截断守卫：单文件最多保留 64KB，总附加文件内容最多保留 256KB，防止撑爆 128K 窗口
	if len(req.InputFiles) > 0 && userContent != "" {
		const (
			maxSingleFileSize = 64 * 1024  // 64KB
			maxTotalExtraSize = 256 * 1024 // 256KB
		)
		totalExtraSize := 0
		for _, f := range req.InputFiles {
			if totalExtraSize >= maxTotalExtraSize {
				userContent += fmt.Sprintf("\n\n--- 附带输入文件 (%s) 已省略 (超出 256KB 上下文保护上限) ---\n", f)
				continue
			}
			absPath := f
			if !filepath.IsAbs(absPath) && req.WorkDir != "" {
				absPath = filepath.Join(req.WorkDir, f)
			}
			if fi, err := os.Stat(absPath); err == nil && !fi.IsDir() {
				if fileBytes, err := os.ReadFile(absPath); err == nil {
					isTruncated := false
					if len(fileBytes) > maxSingleFileSize {
						fileBytes = fileBytes[:maxSingleFileSize]
						isTruncated = true
					}
					// 仅在 userContent 尚未包含文件正文时追加
					contentStr := string(fileBytes)
					if !strings.Contains(userContent, contentStr) {
						if isTruncated {
							contentStr += "\n... [单文件超过 64KB 已自动软截断，保护 128K 上下文] ..."
						}
						userContent += fmt.Sprintf("\n\n--- 附带输入文件内容 (%s) ---\n%s\n", f, contentStr)
						totalExtraSize += len(fileBytes)
					}
				}
			}
		}
	}

	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userContent,
	})

	notify := func(level, stream, format string, args ...any) {
		if req.Observability != nil {
			req.Observability.Console.Append(level, stream, format, args...)
		}
	}

	// 3. 获取可用异构端点候选池
	candidates := n.resolveEndpoints(req.ModelName)
	if len(candidates) == 0 {
		n.recordFailure()
		notify("error", "system", "no available native LLM endpoints configured")
		return NewClassifiedError(ErrorClassConfig, "no available native LLM endpoints configured")
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	baseBackoff := time.Duration(cfg.RetryBackoffMs) * time.Millisecond
	if baseBackoff <= 0 {
		baseBackoff = 500 * time.Millisecond
	}
	var respBody []byte
	var lastErr error
	var finalTokens int64
	var streamedContent string
	var streamSucceeded bool

	// 4. 带异构端点故障转移 (Failover) 与指数退避的重试循环
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ep := candidates[attempt%len(candidates)]

		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(baseBackoff / 2)))
			sleepDur := baseBackoff*(1<<uint(attempt-1)) + jitter
			log.Printf("[NativeInvoker] Retry attempt %d/%d switching to endpoint %q (%s, model: %s) after %v (last error: %v)\n",
				attempt, maxRetries, ep.Name, ep.BaseURL, ep.Model, sleepDur, lastErr)
			notify("debug", "system", "retry attempt=%d endpoint=%s model=%s", attempt+1, ep.Name, ep.Model)
			select {
			case <-ctx.Done():
				n.recordFailure()
				return ctx.Err()
			case <-time.After(sleepDur):
			}
		}

		modelName := ep.Model
		if req.ModelName != "" {
			modelName = req.ModelName
		}
		notify("debug", "system", "native attempt=%d endpoint=%s model=%s", attempt+1, ep.Name, modelName)
		// 动态控制 ResponseFormat：优先使用请求级声明，未指定时回退至全局配置
		isJSONMode := strings.EqualFold(req.ResponseFormat, "json") ||
			(req.ResponseFormat == "" && cfg.ResponseFormatJSON)
		isTextStream := strings.EqualFold(req.ResponseFormat, "text") ||
			strings.EqualFold(req.ResponseFormat, "markdown")
		// JSON 响应同样启用流式：长推理请求依靠首字节/空闲 watchdog 保持连接，
		// 而不是被固定 attempt 总超时掐断。
		useStream := isTextStream || isJSONMode

		nativeMetrics := NativeStreamMetrics{
			Attempt:       attempt + 1,
			Endpoint:      ep.BaseURL,
			StreamEnabled: useStream,
		}
		if req.Metrics != nil {
			req.Metrics.DriverAttempts = attempt + 1
			req.Metrics.DriverFailovers = attempt
		}
		req.Observability.SetNativeMetrics(nativeMetrics)
		requestBody := map[string]interface{}{
			"model":    modelName,
			"messages": messages,
			"stream":   useStream,
		}

		temperature := req.Temperature
		if temperature == nil {
			temperature = models.AppConfig.DeterministicTemperature()
		}
		if temperature != nil {
			requestBody["temperature"] = *temperature
		}

		if responseFormat := nativeResponseFormat(req, isJSONMode); responseFormat != nil {
			requestBody["response_format"] = responseFormat
		}
		if !cfg.EnableThinking {
			requestBody["thinking"] = map[string]string{"type": "disabled"}
			// OpenAI-compatible gateways disagree on the wire field used to
			// disable chat-template reasoning. Send both documented variants;
			// providers ignore template variables they do not consume.
			requestBody["chat_template_kwargs"] = map[string]interface{}{
				"thinking":        false,
				"enable_thinking": false,
			}
		}
		if cfg.MaxTokens > 0 {
			requestBody["max_tokens"] = cfg.MaxTokens
		} else if useStream {
			requestBody["max_tokens"] = defaultNativeTextMaxTokens
		}

		reqBytes, err := json.Marshal(requestBody)
		if err != nil {
			notify("error", "system", "failed to marshal native request: %v", err)
			return fmt.Errorf("failed to marshal native request: %w", err)
		}
		notify("debug", "system", "request bytes=%d json_mode=%t", len(reqBytes), isJSONMode)

		// 流式请求不套固定 attempt 总超时，由 readNativeStreamingChat 内的
		// 首字节/空闲 watchdog 兜底；父 context（整体请求预算）仍然生效。
		// 非流式请求保留固定 attempt 超时作为硬上限。
		var attemptCtx context.Context
		var cancelAttempt context.CancelFunc
		if useStream {
			attemptCtx, cancelAttempt = context.WithCancel(ctx)
		} else {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, nativeAttemptTimeout(cfg, ctx))
		}
		httpReq, err := http.NewRequestWithContext(attemptCtx, "POST", ep.BaseURL, bytes.NewReader(reqBytes))
		if err != nil {
			cancelAttempt()
			notify("error", "system", "failed to create http request: %v", err)
			return fmt.Errorf("failed to create http request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", nativeUserAgent)
		if ep.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
		}

		streamStart := time.Now()
		resp, err := n.client.Do(httpReq)
		if err != nil {
			cancelAttempt()
			nativeMetrics.CancelReason = err.Error()
			req.Observability.SetNativeMetrics(nativeMetrics)
			lastErr = WrapClassifiedError(ErrorClassNetworkTransient, err, "endpoint %q (%s) connection failed", ep.Name, ep.BaseURL)
			notify("error", "system", "%v", lastErr)
			continue
		}

		// 仅当端点真正以 SSE 返回时才走流式解析；若网关忽略了 stream 参数
		// 仍返回普通 JSON，则降级到非流式读取路径。
		if useStream && resp.StatusCode == http.StatusOK && isEventStreamResponse(resp) {
			content, tokens, streamStats, readErr := readNativeStreamingChat(resp, cfg, streamStart, notify)
			resp.Body.Close()
			cancelAttempt()
			nativeMetrics.FirstByteMS = streamStats.FirstByte.Milliseconds()
			nativeMetrics.IdleMS = streamStats.MaxIdle.Milliseconds()
			nativeMetrics.OutputBytes = int64(len(content))
			nativeMetrics.ReasoningBytes = streamStats.ReasoningBytes
			if readErr != nil {
				nativeMetrics.CancelReason = readErr.Error()
			}
			req.Observability.SetNativeMetrics(nativeMetrics)
			if readErr != nil {
				lastErr = WrapClassifiedError(ClassifyError(readErr), readErr, "endpoint %q stream failed", ep.Name)
				notify("error", "system", "%v", lastErr)
				continue
			}
			streamedContent = content
			finalTokens = tokens
			streamSucceeded = true
			lastErr = nil
			break
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		cancelAttempt()
		nativeMetrics.OutputBytes = int64(len(respBody))
		req.Observability.SetNativeMetrics(nativeMetrics)

		if err != nil {
			lastErr = WrapClassifiedError(ErrorClassNetworkTransient, err, "endpoint %q read response failed", ep.Name)
			notify("error", "system", "%v", lastErr)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			lastErr = nil
			notify("debug", "system", "http status=%d response_bytes=%d", resp.StatusCode, len(respBody))
			break
		}

		// 记录错误响应
		lastErr = NewClassifiedError(
			errorClassForHTTPStatus(resp.StatusCode),
			"endpoint %q returned HTTP %d: %s", ep.Name, resp.StatusCode, string(respBody),
		)
		notify("error", "system", "%v", lastErr)

		// 401/403/404 属于特定端点鉴权或路径配置故障，429 或 5xx 属于服务端限流/宕机，均继续 Failover 至下一个候选端点
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			continue
		}

		// 其他 4xx（如 400 Bad Request Payload 无效）通常为全局请求错误，退出循环以尝试本地 CLI 降级
		break
	}

	if lastErr != nil {
		if jsonSchemaFallbackEligible(lastErr, req) {
			if req.Metrics != nil {
				req.Metrics.ResponseFormatFallbacks++
			}
			log.Printf("[NativeInvoker] json_schema unsupported; falling back to json_object (%v)\n", lastErr)
			notify("system", "system", "json_schema unsupported; falling back to json_object")
			retryRequest := req
			retryRequest.JSONSchema = nil
			return n.Invoke(retryRequest)
		}
		n.recordFailure()
		if metrics := req.Observability.NativeMetricsSnapshot(); metrics != nil {
			metrics.FallbackMode = "cli"
			req.Observability.SetNativeMetrics(*metrics)
		}
		log.Printf("[NativeInvoker] Native request failed (%v), attempting fallback to CLI...\n", lastErr)
		notify("error", "system", "native request failed; attempting CLI fallback")
		// 平滑降级至本地 CLI 执行
		if fallbackErr := n.fallbackToCLI(req); fallbackErr == nil {
			if req.Metrics != nil {
				req.Metrics.DriverFailovers++
			}
			log.Printf("[NativeInvoker] CLI fallback succeeded after native failure")
			notify("system", "system", "CLI fallback succeeded")
			return nil
		} else {
			notify("error", "system", "CLI fallback failed: %v", fallbackErr)
			return WrapClassifiedError(
				ClassifyError(lastErr),
				fmt.Errorf("%w; CLI fallback also failed: %v", lastErr, fallbackErr),
				"all native endpoints failed",
			)
		}
	}

	if streamSucceeded {
		n.recordSuccess()
		content := strings.TrimSpace(streamedContent)
		notify("system", "system", "native stream succeeded tokens=%d output_bytes=%d", finalTokens, len(content))
		if req.OutputPath != "" {
			if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0755); err != nil {
				return fmt.Errorf("failed to create output dir: %w", err)
			}
			if err := os.WriteFile(req.OutputPath, []byte(content), 0644); err != nil {
				notify("error", "system", "failed to write native output file: %v", err)
				return fmt.Errorf("failed to write native output file: %w", err)
			}
		}
		log.Printf("[NativeInvoker] Call succeeded. Tokens: %d, Output: %s\n", finalTokens, req.OutputPath)
		return nil
	}

	// 5. 解析响应
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		n.recordFailure()
		notify("error", "system", "failed to decode LLM response: %v", err)
		return WrapClassifiedError(ErrorClassOutputInvalid, err, "failed to decode LLM response (body: %s)", string(respBody))
	}

	if len(chatResp.Choices) == 0 {
		n.recordFailure()
		notify("error", "system", "native LLM returned empty choices")
		return NewClassifiedError(ErrorClassOutputInvalid, "native LLM returned empty choices")
	}

	n.recordSuccess()
	finalTokens = chatResp.Usage.TotalTokens
	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	notify("assistant", "assistant", "%s", content)
	notify("system", "system", "native call succeeded tokens=%d output_bytes=%d", finalTokens, len(content))

	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0755); err != nil {
			return fmt.Errorf("failed to create output dir: %w", err)
		}
		if err := os.WriteFile(req.OutputPath, []byte(content), 0644); err != nil {
			notify("error", "system", "failed to write native output file: %v", err)
			return fmt.Errorf("failed to write native output file: %w", err)
		}
	}

	log.Printf("[NativeInvoker] Call succeeded. Tokens: %d, Output: %s\n", finalTokens, req.OutputPath)
	return nil
}

func jsonSchemaFallbackEligible(lastErr error, req AIRequest) bool {
	if req.JSONSchema == nil || req.JSONSchema.Strict || lastErr == nil {
		return false
	}
	var classified *ClassifiedError
	if !errors.As(lastErr, &classified) {
		return false
	}
	return classified.ErrorClass() == ErrorClassUnknown && strings.Contains(classified.Error(), "HTTP 400")
}

func nativeResponseFormat(req AIRequest, jsonMode bool) map[string]any {
	if !jsonMode {
		return nil
	}
	if req.JSONSchema != nil {
		schema := req.JSONSchema
		if schema.Schema == nil {
			return map[string]any{"type": "json_object"}
		}
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   schema.Name,
				"strict": schema.Strict,
				"schema": schema.Schema,
			},
		}
	}
	return map[string]any{"type": "json_object"}
}

type nativeStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
}

type nativeStreamStats struct {
	FirstByte      time.Duration
	MaxIdle        time.Duration
	ReasoningBytes int64
}

func nativeSeconds(value, fallback int) time.Duration {
	if value <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(value) * time.Second
}

func nativeAttemptTimeout(cfg models.NativeLLMConfig, parent context.Context) time.Duration {
	timeout := nativeSeconds(cfg.AttemptTimeoutSeconds, 300)
	if deadline, ok := parent.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			return remaining
		}
	}
	return timeout
}

func isEventStreamResponse(resp *http.Response) bool {
	return strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
}

func nativeMaxOutputBytes(cfg models.NativeLLMConfig) int {
	if cfg.MaxOutputBytes > 0 {
		return cfg.MaxOutputBytes
	}
	return 32 * 1024
}

func readNativeStreamingChat(resp *http.Response, cfg models.NativeLLMConfig, requestStart time.Time, notify func(level, stream, format string, args ...any)) (string, int64, nativeStreamStats, error) {
	if resp.Body == nil {
		return "", 0, nativeStreamStats{}, NewClassifiedError(ErrorClassOutputInvalid, "nil response body")
	}

	firstByteTimeout := nativeSeconds(cfg.FirstByteTimeoutSeconds, 60)
	idleTimeout := nativeSeconds(cfg.IdleTimeoutSeconds, 60)
	watchdog := time.AfterFunc(firstByteTimeout, func() { _ = resp.Body.Close() })
	defer watchdog.Stop()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var content strings.Builder
	var tokens int64
	var finishReason string
	var firstByteAt time.Time
	var lastEventAt = requestStart
	var stats nativeStreamStats
	sawEvent := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		if !sawEvent {
			sawEvent = true
			watchdog.Reset(idleTimeout)
			notify("debug", "system", "native stream first byte")
		} else {
			watchdog.Reset(idleTimeout)
		}

		eventAt := time.Now()
		if firstByteAt.IsZero() {
			firstByteAt = eventAt
			stats.FirstByte = eventAt.Sub(requestStart)
		} else if idle := eventAt.Sub(lastEventAt); idle > stats.MaxIdle {
			stats.MaxIdle = idle
		}
		lastEventAt = eventAt

		var chunk nativeStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return "", tokens, stats, WrapClassifiedError(ErrorClassOutputInvalid, err, "decode stream chunk")
		}
		if chunk.Usage.TotalTokens > tokens {
			tokens = chunk.Usage.TotalTokens
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				notify("assistant", "assistant", "%s", choice.Delta.Content)
			}
			stats.ReasoningBytes += int64(len(choice.Delta.Reasoning))
			if limit := nativeMaxOutputBytes(cfg); limit > 0 && content.Len() > limit {
				return "", tokens, stats, NewClassifiedError(ErrorClassOutputInvalid, "stream output exceeded %d bytes", limit)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", tokens, stats, WrapClassifiedError(ErrorClassNetworkTransient, err, "read stream")
	}
	if !sawEvent {
		if time.Since(requestStart) >= firstByteTimeout {
			return "", tokens, stats, NewClassifiedError(ErrorClassIdleTimeout, "stream closed before first byte after %v", firstByteTimeout)
		}
		return "", tokens, stats, NewClassifiedError(ErrorClassOutputMissing, "stream closed before first event")
	}
	if finishReason == "length" {
		return "", tokens, stats, NewClassifiedError(ErrorClassOutputInvalid, "stream output reached max_tokens before completion")
	}
	if strings.TrimSpace(content.String()) == "" {
		if time.Since(lastEventAt) >= idleTimeout {
			return "", tokens, stats, NewClassifiedError(ErrorClassIdleTimeout, "stream completed without content after %v idle", idleTimeout)
		}
		return "", tokens, stats, NewClassifiedError(ErrorClassOutputMissing, "stream completed without content")
	}
	return strings.TrimSpace(content.String()), tokens, stats, nil
}

func init() {
	RegisterAIInvoker("native", NewNativeInvoker())
}
