package services

import (
	"bytes"
	"context"
	"encoding/json"
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
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: 5 * time.Second,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
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
	if len(cfg.Endpoints) > 0 {
		var matched []models.NativeEndpointConfig
		// 若指定了目标模型，优先筛选匹配该模型的端点
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

	// 兼容单端点简写配置
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = cfg.BaseURL
	}
	if endpoint == "" {
		endpoint = "http://192.168.56.18:8000/v1/chat/completions"
	}
	model := targetModel
	if model == "" {
		model = cfg.DefaultModel
		if model == "" {
			model = "glm-4-flash"
		}
	}
	return []models.NativeEndpointConfig{
		{
			Name:       "default",
			BaseURL:    endpoint,
			APIKey:     cfg.APIKey,
			Model:      model,
			Weight:     100,
			Concurrent: 20,
		},
	}
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

// fallbackToCLI 当 Native 引擎熔断或不可用时，平滑降级调用本地 CLI 后端
func (n *NativeInvoker) fallbackToCLI(req AIRequest) error {
	fallbackBackend := models.AppConfig.AI.Backend
	if fallbackBackend == "" || fallbackBackend == "native" {
		fallbackBackend = "claude"
	}
	log.Printf("[NativeInvoker] Falling back request to CLI backend %q\n", fallbackBackend)

	invoker, ok := invokerRegistry[fallbackBackend]
	if !ok || invoker == nil {
		return fmt.Errorf("circuit breaker fallback failed: CLI backend %q not registered", fallbackBackend)
	}
	return invoker.Invoke(req)
}

func (n *NativeInvoker) Invoke(req AIRequest) error {
	// 1. 检查断路器，若已熔断直接平滑降级至本地 CLI
	if n.checkCircuitBreaker() {
		log.Println("[NativeInvoker] Circuit breaker is OPEN, routing directly to CLI fallback")
		return n.fallbackToCLI(req)
	}

	ctx := req.ParentContext
	if ctx == nil {
		ctx = context.Background()
	}

	timeoutMin := req.TimeoutMin
	if timeoutMin <= 0 {
		timeoutMin = 10
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMin)*time.Minute)
	defer cancel()

	cfg := models.AppConfig.AI.Native

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
	if len(req.InputFiles) > 0 && userContent != "" {
		for _, f := range req.InputFiles {
			absPath := f
			if !filepath.IsAbs(absPath) && req.WorkDir != "" {
				absPath = filepath.Join(req.WorkDir, f)
			}
			if fi, err := os.Stat(absPath); err == nil && !fi.IsDir() && fi.Size() < 512*1024 {
				if fileBytes, err := os.ReadFile(absPath); err == nil {
					// 仅在 userContent 尚未包含文件正文时追加
					if !strings.Contains(userContent, string(fileBytes)) {
						userContent += fmt.Sprintf("\n\n--- 附带输入文件内容 (%s) ---\n%s\n", f, string(fileBytes))
					}
				}
			}
		}
	}

	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userContent,
	})

	// 3. 获取可用异构端点候选池
	candidates := n.resolveEndpoints(req.ModelName)
	if len(candidates) == 0 {
		n.recordFailure()
		return fmt.Errorf("no available native LLM endpoints configured")
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

	// 4. 带异构端点故障转移 (Failover) 与指数退避的重试循环
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ep := candidates[attempt%len(candidates)]

		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(baseBackoff / 2)))
			sleepDur := baseBackoff*(1<<uint(attempt-1)) + jitter
			log.Printf("[NativeInvoker] Retry attempt %d/%d switching to endpoint %q (%s, model: %s) after %v (last error: %v)\n",
				attempt, maxRetries, ep.Name, ep.BaseURL, ep.Model, sleepDur, lastErr)
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
		temperature := cfg.Temperature
		if ep.Temperature > 0 {
			temperature = ep.Temperature
		}

		requestBody := map[string]interface{}{
			"model":       modelName,
			"messages":    messages,
			"temperature": temperature,
		}
		if cfg.ResponseFormatJSON {
			requestBody["response_format"] = map[string]string{"type": "json_object"}
		}
		if cfg.MaxTokens > 0 {
			requestBody["max_tokens"] = cfg.MaxTokens
		}

		reqBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal native request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.BaseURL, bytes.NewReader(reqBytes))
		if err != nil {
			return fmt.Errorf("failed to create http request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if ep.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
		}

		resp, err := n.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %q (%s) connection failed: %w", ep.Name, ep.BaseURL, err)
			continue
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("endpoint %q read response failed: %w", ep.Name, err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			lastErr = nil
			break
		}

		// 记录错误响应
		lastErr = fmt.Errorf("endpoint %q returned HTTP %d: %s", ep.Name, resp.StatusCode, string(respBody))

		// 401/403/404 属于特定端点鉴权或路径配置故障，429 或 5xx 属于服务端限流/宕机，均继续 Failover 至下一个候选端点
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			continue
		}

		// 其他 4xx（如 400 Bad Request Payload 无效）通常为全局请求错误，退出循环以尝试本地 CLI 降级
		break
	}

	if lastErr != nil {
		n.recordFailure()
		log.Printf("[NativeInvoker] Native request failed (%v), attempting fallback to CLI...\n", lastErr)
		// 平滑降级至本地 CLI 执行
		if fallbackErr := n.fallbackToCLI(req); fallbackErr == nil {
			log.Printf("[NativeInvoker] CLI fallback succeeded after native failure")
			return nil
		} else {
			return fmt.Errorf("all native endpoints failed (%w), and CLI fallback also failed: %v", lastErr, fallbackErr)
		}
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
		return fmt.Errorf("failed to decode LLM response: %w (body: %s)", err, string(respBody))
	}

	if len(chatResp.Choices) == 0 {
		n.recordFailure()
		return fmt.Errorf("native LLM returned empty choices")
	}

	n.recordSuccess()
	finalTokens = chatResp.Usage.TotalTokens
	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)

	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0755); err != nil {
			return fmt.Errorf("failed to create output dir: %w", err)
		}
		if err := os.WriteFile(req.OutputPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write native output file: %w", err)
		}
	}

	log.Printf("[NativeInvoker] Call succeeded. Tokens: %d, Output: %s\n", finalTokens, req.OutputPath)
	return nil
}

func init() {
	RegisterAIInvoker("native", NewNativeInvoker())
}
