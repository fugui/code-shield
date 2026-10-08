package runner

import (
	"log"
	"time"

	"code-shield/models"
	"code-shield/services/invoker"
)

func recordChunkExecution(
	ctx *TaskContext,
	files []string,
	attempt int,
	startedAt time.Time,
	invocation *InvocationObservation,
	isFinal bool,
	err error,
) {
	if models.DB == nil || ctx.Report.ID == 0 || ctx.ChunkUID == "" || attempt <= 0 {
		return
	}

	now := time.Now()
	class := invoker.ErrorClassNone
	if err != nil {
		class = invoker.ClassifyError(err)
	}
	status := models.StatusSuccess
	if err != nil {
		status = models.StatusFailed
	}

	execution := models.TaskChunkExecution{
		ReportID:      ctx.Report.ID,
		RepoID:        ctx.Repo.ID,
		TaskTypeID:    ctx.TaskType.ID,
		ChunkUID:      ctx.ChunkUID,
		ChunkName:     ctx.Report.ChunkName,
		PrimaryUnitID: primaryUnitID(files),
		FilePath:      firstFilePath(files),
		Stage:         "analysis",
		Attempt:       attempt,
		AttemptKind:   "invocation",
		IsFinal:       isFinal,
		Status:        status,
		ErrorClass:    string(class),
		ErrorMessage:  errorMessage(err),
		StartedAt:     startedAt,
		FinishedAt:    &now,
	}
	if execution.ErrorClass == "" {
		execution.ErrorClass = string(invoker.ErrorClassNone)
	}
	if attempt > 1 && execution.AttemptKind == "invocation" {
		execution.AttemptKind = "fresh_retry"
	}
	if invocation != nil {
		execution.Driver = invocation.Driver
		execution.Backend = invocation.Backend
		execution.ResourceID = invocation.ResourceID
		execution.ModelName = invocation.ModelName
		execution.PromptHash = invocation.PromptHash
		execution.ArtifactHash = invocation.ArtifactHash
		execution.ArtifactPath = artifactPath(invocation, ctx.JsonPath)
		execution.QueueWaitMs = invocation.QueueWaitMs
		execution.DurationMs = invocation.DurationMs
		if invocation.Continuations > 0 {
			execution.AttemptKind = "driver_continuation"
		}
		if invocation.DriverFailovers > 0 {
			execution.AttemptKind = "cross_resource_failover"
		}
	}
	if execution.DurationMs == 0 {
		execution.DurationMs = now.Sub(startedAt).Milliseconds()
	}
	if len(execution.TokenUsage) == 0 {
		execution.TokenUsage = []byte("{}")
	}

	if updateErr := models.UpsertChunkExecution(models.DB, &execution); updateErr != nil {
		log.Printf(
			"[ChunkExecution] Failed to persist execution report=%d chunk=%s attempt=%d: %v",
			ctx.Report.ID, ctx.ChunkUID, attempt, updateErr,
		)
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func artifactPath(invocation *InvocationObservation, jsonPath string) string {
	if invocation != nil && invocation.ArtifactPath != "" {
		return invocation.ArtifactPath
	}
	if jsonPath == "" {
		return ""
	}
	return jsonPath + ".raw"
}

func firstFilePath(files []string) string {
	for _, file := range files {
		if file != "" {
			return file
		}
	}
	return ""
}
