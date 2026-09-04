package engines

const (
	// DefaultChunkMaxFiles 默认单分片文件数上限。
	// 经架构调优收敛至 8 个文件（6~8 文件黄金区间），有效防止大模型上下文注意力衰减与长尾文件漏扫。
	DefaultChunkMaxFiles    = 8
	DefaultChunkDepth       = 1
	DefaultChunkConcurrency = 6
)

// ChunkConfig 定义分片引擎与扫描切片的共享配置参数
type ChunkConfig struct {
	MaxFiles        int      `json:"max_files"`
	Depth           int      `json:"depth"`
	Concurrency     int      `json:"concurrency"`
	SinceDays       int      `json:"since_days"`       // 增量检视：仅检视最近 N 天内有提交的文件 (例如 7 天)
	DiffBase        string   `json:"diff_base"`        // 增量检视：仅检视与基准 commit/分支有 diff 的文件 (例如 origin/main)
	FileExtensions  []string `json:"file_extensions"`  // 任务级文件扩展名白名单，为空时使用全局 sourceExtensions
	ContentKeywords []string `json:"content_keywords"` // 任务级文件内容关键字白名单，只有当文件内容包含其中任意关键字时才进行分析
	ExcludePaths    []string `json:"exclude_paths"`    // 任务级忽略路径
}
