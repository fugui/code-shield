package services

import (
	"code-shield/models"
	"testing"
)

func TestDiffClassification(t *testing.T) {
	// 验证常量定义
	if models.DiffStatusNew != "NEW" {
		t.Errorf("Expected NEW, got %s", models.DiffStatusNew)
	}
	if models.DiffStatusExisted != "EXISTED" {
		t.Errorf("Expected EXISTED, got %s", models.DiffStatusExisted)
	}
	if models.DiffStatusResolved != "RESOLVED" {
		t.Errorf("Expected RESOLVED, got %s", models.DiffStatusResolved)
	}
	if models.DiffStatusReopened != "REOPENED" {
		t.Errorf("Expected REOPENED, got %s", models.DiffStatusReopened)
	}
}
