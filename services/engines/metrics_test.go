package engines

import "testing"

func TestAggregateAnalysisMetrics(t *testing.T) {
	metrics := AggregateAnalysisMetrics([]ChunkDetails{
		{Attempts: 2, Retries: 1, ContractRepairs: 1, DriverFailovers: 2, SplitDepth: 3, SplitCount: 4, Resumed: false},
		{
			Attempts: 4, Retries: 1, ContractRepairs: 2, DriverFailovers: 1, SplitDepth: 2, Resumed: true,
			ArtifactSchemaID: "schema-2", ArtifactSchemaHash: "sha256:b",
			ResponseFormatMode: "json_object", ResponseFormatFallbacks: 2,
		},
	})

	if metrics.Attempts != 6 || metrics.Retries != 2 {
		t.Fatalf("attempt/retry metrics = (%d, %d), want (6, 2)", metrics.Attempts, metrics.Retries)
	}
	if metrics.ContractRepairs != 3 || metrics.DriverFailovers != 3 {
		t.Fatalf("repair/failover metrics = (%d, %d), want (3, 3)", metrics.ContractRepairs, metrics.DriverFailovers)
	}
	if metrics.SplitInvocations != 6 || metrics.ResumedChunks != 1 {
		t.Fatalf("split/recovered metrics = (%d, %d), want (6, 1)", metrics.SplitInvocations, metrics.ResumedChunks)
	}
	if metrics.ArtifactSchemaID != "schema-2" || metrics.ArtifactSchemaHash != "sha256:b" ||
		metrics.ResponseFormatMode != "json_object" || metrics.ResponseFormatFallbacks != 2 {
		t.Fatalf("native schema metrics = %+v", metrics)
	}
}
