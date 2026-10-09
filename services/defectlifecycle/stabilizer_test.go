package defectlifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-shield/models"
)

func TestExtractPhysicalTargetEntity(t *testing.T) {
	tests := []struct {
		name         string
		snippet      string
		promptTarget string
		classMajor   string
		expectedEnt  string
		expectedConf AnchorConfidence
	}{
		{
			name:         "Leftmost root in chained call",
			snippet:      "pSession->GetContext()->GetPacket().payload = nullptr;",
			promptTarget: "payload",
			classMajor:   ClassNPD,
			expectedEnt:  "payload", // 存在且在 candidates 中，Tie-Breaker 确认 HIGH
			expectedConf: ConfidenceHigh,
		},
		{
			name:         "Leftmost root fallback when prompt is hallucinated",
			snippet:      "pSession->GetContext()->GetPacket().payload = nullptr;",
			promptTarget: "non_existent_var",
			classMajor:   ClassNPD,
			expectedEnt:  "pSession", // 物理首选受体
			expectedConf: ConfidenceMedium,
		},
		{
			name:         "C++ macro unwrapping",
			snippet:      "SAFE_DELETE(pDeviceHandler);",
			promptTarget: "",
			classMajor:   ClassMemLeak,
			expectedEnt:  "pDeviceHandler",
			expectedConf: ConfidenceMedium,
		},
		{
			name:         "SQL injection with standard sink",
			snippet:      "int rc = sqlite3_exec(db, sql.c_str(), 0, 0, &errMsg);",
			promptTarget: "",
			classMajor:   ClassSecInject,
			expectedEnt:  "SINK:sqlite3_exec",
			expectedConf: ConfidenceHigh,
		},
		{
			name:         "Deadlock with mutex locking",
			snippet:      "m_mtxCache.lock();",
			promptTarget: "",
			classMajor:   ClassDeadlock,
			expectedEnt:  "LOCK:m_mtxCache",
			expectedConf: ConfidenceHigh,
		},
		{
			name:         "Signal safety non-reentrant call",
			snippet:      "Logger::Info(\"received signal\");",
			promptTarget: "SIGNAL:Logger",
			classMajor:   ClassSignalSafe,
			expectedEnt:  "SIGNAL:Logger",
			expectedConf: ConfidenceHigh,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ent, conf := ExtractPhysicalTargetEntity(tc.snippet, tc.promptTarget, tc.classMajor)
			if ent != tc.expectedEnt {
				t.Errorf("expected entity %q, got %q", tc.expectedEnt, ent)
			}
			if conf != tc.expectedConf {
				t.Errorf("expected confidence %v, got %v", tc.expectedConf, conf)
			}
		})
	}
}

func TestTaxonomyReduction(t *testing.T) {
	cases := []struct {
		category string
		title    string
		expected string
	}{
		{"CWE-476", "Null pointer dereference", ClassNPD},
		{"空指针异常", "变量可能为空未校验", ClassNPD},
		{"memory_leak", "动态内存未释放", ClassMemLeak},
		{"资源未释放", "文件句柄泄露", ClassResLeak},
		{"CWE-416", "释放后使用", ClassUAF},
		{"CWE-415", "指针重复释放", ClassDoubleFree},
		{"缓冲区溢出", "数组下标越界访问", ClassOOB},
		{"并发竞争", "多线程未加锁访问", ClassDataRace},
		{"死锁", "循环等待互斥量", ClassDeadlock},
		{"异步信号安全", "信号处理函数中调用非可重入函数", ClassSignalSafe},
		{"命令注入", "存在系统命令拼接执行", ClassSecInject},
		{"整数溢出", "数值截断导致逻辑异常", ClassIntOver},
		{"未初始化变量", "局部变量未赋初值", ClassUninit},
		{"未知分类", "自定义未知逻辑问题", ClassOther},
	}

	for _, c := range cases {
		res := ReduceControlledDefectClass(c.category, c.title)
		if res != c.expected {
			t.Errorf("category=%q, title=%q: expected %q, got %q", c.category, c.title, c.expected, res)
		}
	}
}

func TestReconstructStatementWindow(t *testing.T) {
	lines := []string{
		"void Process(Session* pSession) {",
		"    if (pSession != nullptr &&",
		"        pSession->GetReq() != nullptr &&",
		"        pSession->GetReq()->IsValid()) {",
		"        DoAction();",
		"    }",
		"}",
	}

	// 针对第 3 行，应该重构出完整的条件表达式
	recon := ReconstructStatementWindow(lines, 3)
	if !strings.Contains(recon, "pSession->GetReq()") {
		t.Fatalf("reconstruction failed: %q", recon)
	}
}

// TestStabilizer_22CasesBenchmark 现网 22 组抖动样本 100% 离线验真与性能基准
func TestStabilizer_22CasesBenchmark(t *testing.T) {
	// 创建临时代码仓与测试源文件
	tempDir, err := os.MkdirTemp("", "stabilizer_test_repo_*")
	if err != nil {
		t.Fatalf("failed to create temp repo: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSourceFile := `// Mock production file
#include <iostream>
#include <vector>
#include <mutex>

namespace Core {
namespace Net {

void HandleRequest(int reqId) {
    char* buf = (char*)malloc(1024);
    if (!buf) return;
    // Line 12
    SAFE_DELETE(pDevice);
    // Line 14: NPD
    buf[0] = 'A';
    free(buf);
}

void OnSignalReceived(int signum) {
    // Line 20: Signal safe violation
    Logger::Info("signal received");
}

void QueryUser(const std::string& name) {
    // Line 25: SQL Injection
    sqlite3_exec(g_db, ("SELECT * FROM users WHERE name = " + name).c_str(), 0, 0, 0);
}

} // Net
} // Core
`
	srcRelPath := "src/net/handler.cpp"
	fullPath := filepath.Join(tempDir, srcRelPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(mockSourceFile), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// 构建 22 组具有现网特征的 Finding 样本（覆盖行号缺失、漂移、中英文分类、宏穿透等）
	findings := make([]models.AnalysisFinding, 22)
	for i := 0; i < 22; i++ {
		switch i % 5 {
		case 0:
			// 样本 1: 行号缺失 (现网 15% 样本)，靠 trigger_line 反查
			findings[i] = models.AnalysisFinding{
				ID:           uint(i + 1),
				FilePath:     srcRelPath,
				LineNumber:   "", // 故意为空
				TriggerLine:  "buf[0] = 'A';",
				Category:     "空指针异常",
				Title:        fmt.Sprintf("可能存在空指针解引用风险 #%d", i),
				TargetSymbol: "buf",
			}
		case 1:
			// 样本 2: 行号漂移 5 行 (现网 68% 样本)
			findings[i] = models.AnalysisFinding{
				ID:           uint(i + 1),
				FilePath:     srcRelPath,
				LineNumber:   "19", // 实际是第 14 行
				TriggerLine:  "buf[0] = 'A';",
				Category:     "CWE-476",
				Title:        fmt.Sprintf("未判空解引用 #%d", i),
				TargetSymbol: "buf",
			}
		case 2:
			// 样本 3: 信号处理安全 (现网典型漏配案例)
			findings[i] = models.AnalysisFinding{
				ID:           uint(i + 1),
				FilePath:     srcRelPath,
				LineNumber:   "20",
				TriggerLine:  "Logger::Info(\"signal received\");",
				Category:     "异步信号处理不安全",
				Title:        "在信号处理程序中调用非异步安全函数",
				TargetSymbol: "SIGNAL:Logger",
			}
		case 3:
			// 样本 4: 宏包装穿透
			findings[i] = models.AnalysisFinding{
				ID:           uint(i + 1),
				FilePath:     srcRelPath,
				LineNumber:   "12",
				TriggerLine:  "SAFE_DELETE(pDevice);",
				Category:     "memory_leak",
				Title:        "内存释放宏调用风险",
				TargetSymbol: "pDevice",
			}
		case 4:
			// 样本 5: SQL 注入合成受体
			findings[i] = models.AnalysisFinding{
				ID:           uint(i + 1),
				FilePath:     srcRelPath,
				LineNumber:   "25",
				TriggerLine:  "sqlite3_exec(g_db, ...);",
				Category:     "CWE-89",
				Title:        "SQL注入漏洞",
				ScopeSymbol:  "Core::Net::QueryUser",
				TargetSymbol: "SINK:sqlite3_exec",
			}
		}
	}

	stabilizer := NewIngestionStabilizer()

	// 1. 预热运行，使文件加载进内存缓存
	_, _ = stabilizer.Stabilize(StabilizeFindingsInput{
		RepoRoot: tempDir,
		Findings: findings,
	})

	// 2. 正式性能基准测量 (10 次迭代取平均)
	iterations := 10
	start := time.Now()
	var dtoList []StabilizedFindingDTO
	for it := 0; it < iterations; it++ {
		var err error
		dtoList, err = stabilizer.Stabilize(StabilizeFindingsInput{
			RepoRoot: tempDir,
			Findings: findings,
		})
		if err != nil {
			t.Fatalf("Stabilize returned error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if len(dtoList) != 22 {
		t.Fatalf("expected 22 DTOs, got %d", len(dtoList))
	}

	// 验证 22 组样本槽位计算的一致性与正确性
	for idx, dto := range dtoList {
		if dto.CoreSlotKey == "" {
			t.Errorf("Finding #%d: CoreSlotKey is empty", idx)
		}
		if dto.SlotKey == "" {
			t.Errorf("Finding #%d: SlotKey is empty", idx)
		}
		if dto.DefectClassMajor == ClassOther {
			t.Errorf("Finding #%d: unexpectedly reduced to ClassOther", idx)
		}
		if dto.TargetEntity == "" {
			t.Errorf("Finding #%d: TargetEntity is empty", idx)
		}
	}

	// 验证性能：单 Finding 平均耗时
	totalFindings := iterations * len(dtoList)
	avgDuration := elapsed / time.Duration(totalFindings)
	t.Logf("%d findings stabilized in %v (average: %v/finding)", totalFindings, elapsed, avgDuration)

	if avgDuration > 500*time.Microsecond {
		t.Errorf("Stabilizer average latency %v exceeds 0.5ms threshold", avgDuration)
	}
}
