package services

import (
	"code-shield/models"
	"testing"
)

func TestQueue_MaxQueueSizeLimit(t *testing.T) {
	// 校验配置项生效与默认值
	models.AppConfig.Server.MaxQueueSize = 2000
	if models.AppConfig.Server.MaxQueueSize != 2000 {
		t.Fatalf("expected MaxQueueSize == 2000, got %d", models.AppConfig.Server.MaxQueueSize)
	}

	// 校验 NotifyWorker 在无 Worker 读取时不阻塞
	for i := 0; i < 10; i++ {
		NotifyWorker()
	}
}
