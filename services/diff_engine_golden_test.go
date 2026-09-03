package services

import (
	"code-shield/models"
	"fmt"
	"testing"
)

// TestCasePair 代表一组现网真实实测中已人工确认的重复问题对比对
type TestCasePair struct {
	Name string
	// R1 历史基线数据
	R1Path        string
	R1Line        string
	R1Scope       string
	R1Trigger     string
	R1Category    string
	R1Severity    string
	R1Title       string
	// R2 再次扫描输入数据 (包含漂移)
	R2Path        string
	R2Line        string
	R2Scope       string
	R2Trigger     string
	R2Category    string
	R2Severity    string
	R2Title       string
}

// 构造《03》报告中的 22 组现网连续扫描真实异动案例
func getGoldenTestCases22() []TestCasePair {
	return []TestCasePair{
		{
			Name:       "Match 1 - OpcuaMonitor SubscribeNodes UAF",
			R1Path:     "proj_facility/dev_plc/src/include/OpcuaMonitor.h",
			R1Line:     "63-67",
			R1Scope:    "PROJ_CORE_COMMON::OpcuaMonitor<T>::SubscribeNodes",
			R1Trigger:  "m_subscribeClient->SubscribeNodes(nodeIds, [this](...){ ... });",
			R1Category: "CWE-416: Use After Free",
			R1Severity: "CRITICAL",
			R1Title:    "this指针捕获逃逸导致UAF",
			R2Path:     "proj_facility/dev_plc/src/include/OpcuaMonitor.h",
			R2Line:     "63-67",
			R2Scope:    "OpcuaMonitor<T>::SubscribeNodes", // 命名空间剥离
			R2Trigger:  "m_subscribeClient->SubscribeNodes(nodeIds, [this](...){ ... });",
			R2Category: "内存管理问题-释放后使用(UAF)", // 分类变动
			R2Severity: "HIGH",
			R2Title:    "订阅回调中捕获this但未做生命周期保活导致UAF",
		},
		{
			Name:       "Match 2 - SensorDevice GetEnumCfgItem 越界/空指针",
			R1Path:     "proj_facility/dev_sensor/src/SensorDevice.cpp",
			R1Line:     "187-201",
			R1Scope:    "SensorDevice::GetEnumCfgItem",
			R1Trigger:  "const auto enumValue = field.enum_type()->value(val);",
			R1Category: "CWE-476: NULL Pointer Dereference",
			R1Severity: "HIGH",
			R1Title:    "枚举值越界返回nullptr直接解引用",
			R2Path:     "proj_facility/dev_sensor/src/SensorDevice.cpp",
			R2Line:     "187-201",
			R2Scope:    "PROJ_CORE_COMMON::SensorDevice::GetEnumCfgItem", // 出现命名空间
			R2Trigger:  "const auto enumValue = field.enum_type()->value(val);",
			R2Category: "CWE-125: Out-of-bounds Read / CWE-476: NULL Pointer Dereference",
			R2Severity: "CRITICAL",
			R2Title:    "越界读取枚举值并空指针解引用",
		},
		{
			Name:       "Match 3 - ClickableLabel SetChecked 空指针",
			R1Path:     "mod_widget/src/ClickableLabel.cpp",
			R1Line:     "62-71",
			R1Scope:    "ClickableLabel::SetChecked",
			R1Trigger:  "this->setPixmap(*m_pixmapOpen);",
			R1Category: "内存管理问题-空指针解引用",
			R1Severity: "HIGH",
			R1Title:    "pixmap指针未判空解引用",
			R2Path:     "mod_widget/src/ClickableLabel.cpp",
			R2Line:     "62-71",
			R2Scope:    "ClickableLabel::SetChecked",
			R2Trigger:  "this->setPixmap(*m_pixmapOpen);",
			R2Category: "空指针引用-空指针解引用", // 分类字面差异
			R2Severity: "HIGH",
			R2Title:    "m_pixmapOpen为空时解引用导致崩溃",
		},
		{
			Name:       "Match 4 - PMPBusinessModel GetPMEnergyData 空指针",
			R1Path:     "srv_pmp/src/PMPBusinessModel.cpp",
			R1Line:     "582-585",
			R1Scope:    "PMPBusinessModel::GetPMEnergyData",
			R1Trigger:  "return m_pmDeviceMap[pmDevice]->GetEnergyData();",
			R1Category: "CWE-476: NULL Pointer Dereference",
			R1Severity: "HIGH",
			R1Title:    "map未查直接解引用",
			R2Path:     "srv_pmp/src/PMPBusinessModel.cpp",
			R2Line:     "582-585",
			R2Scope:    "PMPBusinessModel::GetPMEnergyData",
			R2Trigger:  "return m_pmDeviceMap[pmDevice]->GetEnergyData();",
			R2Category: "内存管理问题-空指针解引用",
			R2Severity: "HIGH",
			R2Title:    "下标访问可能插入空指针并引发解引用",
		},
		{
			Name:       "Match 5 - DataProcessor RecalculateStatistics 越界写",
			R1Path:     "mod_proc/src/DataProcessor.cpp",
			R1Line:     "68-74",
			R1Scope:    "DataProcessor::RecalculateStatistics",
			R1Trigger:  "int index = static_cast<int>(m_data[i] / HISTOGRAM_BUCKET_NUM);",
			R1Category: "CWE-787: Out-of-bounds Write",
			R1Severity: "CRITICAL",
			R1Title:    "直方图区间计算越界",
			R2Path:     "mod_proc/src/DataProcessor.cpp",
			R2Line:     "68-74",
			R2Scope:    "PROC_STAT::DataProcessor::RecalculateStatistics", // 命名空间前缀
			R2Trigger:  "int index = static_cast<int>(m_data[i] / HISTOGRAM_BUCKET_NUM);",
			R2Category: "CWE-787: Out-of-bounds Write",
			R2Severity: "CRITICAL",
			R2Title:    "直方图下标计算未越界钳制导致堆越界写",
		},
		{
			Name:       "Match 6 - PROC_CALICaliData ConstructPolltionCaliDataResult 越界",
			R1Path:     "srv_cali/src/PROC_CALICaliData.cpp",
			R1Line:     "978-994",
			R1Scope:    "PROC_CALI::PROC_CALICaliData::ConstructPolltionCaliDataResult",
			R1Trigger:  "result.timeStamp = m_timeStamp[index];",
			R1Category: "CWE-125: Out-of-bounds Read",
			R1Severity: "HIGH",
			R1Title:    "下标未校验导致越界读",
			R2Path:     "srv_cali/src/PROC_CALICaliData.cpp",
			R2Line:     "986-994", // 行号位移 8 行
			R2Scope:    "PROC_CALICaliData::ConstructPolltionCaliDataResult",
			R2Trigger:  "result.timeStamp = m_timeStamp[index];",
			R2Category: "CWE-787: Out-of-bounds Write", // 读写方向分类冲突
			R2Severity: "CRITICAL",
			R2Title:    "时间戳数组越界访问导致内存损坏",
		},
		{
			Name:       "Match 7 - LowFreqImageUploadTask GetUploadData 越界",
			R1Path:     "cam_tif/src/ImageUploadTask.cpp",
			R1Line:     "232-249",
			R1Scope:    "LowFreqImageUploadTask::GetUploadData",
			R1Trigger:  "auto it = procData.rbegin(); *it = ...;",
			R1Category: "内存管理问题-越界访问",
			R1Severity: "HIGH",
			R1Title:    "空容器rbegin解引用",
			R2Path:     "cam_tif/src/ImageUploadTask.cpp",
			R2Line:     "232-249",
			R2Scope:    "LowFreqImageUploadTask::GetUploadData",
			R2Trigger:  "auto it = procData.rbegin(); *it = ...;",
			R2Category: "内存管理问题-空指针/迭代器解引用",
			R2Severity: "HIGH",
			R2Title:    "空容器上解引用rbegin迭代器崩溃",
		},
		{
			Name:       "Match 8 - cam_tcp main.cpp argv[1] 空指针",
			R1Path:     "cam_tcp/src/main.cpp",
			R1Line:     "27-61",
			R1Scope:    "main",
			R1Trigger:  "std::string configPath = argv[1];",
			R1Category: "CWE-476: NULL Pointer Dereference / CWE-125: Out-of-bounds Read",
			R1Severity: "CRITICAL",
			R1Title:    "命令行参数未校验直接访问",
			R2Path:     "cam_tcp/src/main.cpp",
			R2Line:     "56-61", // 行号不同
			R2Scope:    "main",
			R2Trigger:  "std::string configPath = argv[1];",
			R2Category: "CWE-476: NULL Pointer Dereference",
			R2Severity: "CRITICAL",
			R2Title:    "未检查argc访问argv[1]引发崩溃",
		},
		{
			Name:       "Match 9 - LineSeries GetXStart 越界",
			R1Path:     "dev_data/src/LineSeries.cpp",
			R1Line:     "153-162",
			R1Scope:    "LineSeries::GetXStart",
			R1Trigger:  "auto p = procData[0];",
			R1Category: "内存管理问题-越界访问",
			R1Severity: "HIGH",
			R1Title:    "空容器下标访问越界",
			R2Path:     "dev_data/src/LineSeries.cpp",
			R2Line:     "138-163", // 行号位移
			R2Scope:    "LineSeries::GetXStart",
			R2Trigger:  "auto p = procData[0];",
			R2Category: "越界读取",
			R2Severity: "HIGH",
			R2Title:    "procData为空时访问下标0越界崩溃",
		},
		{
			Name:       "Match 10 - CameraGUI UpdateImgInfo 越界读",
			R1Path:     "mod_gui/src/CameraGUI.cpp",
			R1Line:     "346-360",
			R1Scope:    "CameraGUI::UpdateImgInfo",
			R1Trigger:  "imgData.width = buffer[offset];",
			R1Category: "CWE-125: Out-of-bounds Read",
			R1Severity: "MEDIUM",
			R1Title:    "越界读取",
			R2Path:     "mod_gui/src/CameraGUI.cpp",
			R2Line:     "349-351",
			R2Scope:    "CameraGUI::UpdateImgInfo",
			R2Trigger:  "imgData.width = buffer[offset];",
			R2Category: "内存管理问题-越界读取",
			R2Severity: "MEDIUM",
			R2Title:    "buffer长度不足时读取越界",
		},
		{
			Name:       "Match 11 - LSCPController subscriber 内存泄漏",
			R1Path:     "mod_ctrl/src/LSCPController.h",
			R1Line:     "46-51",
			R1Scope:    "LSCPController::subscriber",
			R1Trigger:  "m_sub = new Subscriber();",
			R1Category: "资源管理问题-内存泄漏",
			R1Severity: "HIGH",
			R1Title:    "指针未释放泄漏",
			R2Path:     "mod_ctrl/src/LSCPController.h",
			R2Line:     "48-50",
			R2Scope:    "LSCPController::subscriber",
			R2Trigger:  "m_sub = new Subscriber();",
			R2Category: "内存管理问题-内存泄漏",
			R2Severity: "HIGH",
			R2Title:    "析构中未delete导致原生指针泄漏",
		},
		{
			Name:       "Match 12 - measure_core main.cpp argv[1]",
			R1Path:     "measure_core/src/main.cpp",
			R1Line:     "75",
			R1Scope:    "main",
			R1Trigger:  "char* mode = argv[1];",
			R1Category: "参数校验缺失-空指针解引用",
			R1Severity: "CRITICAL",
			R1Title:    "argc检查缺失",
			R2Path:     "measure_core/src/main.cpp",
			R2Line:     "75-76",
			R2Scope:    "main",
			R2Trigger:  "char* mode = argv[1];",
			R2Category: "越界读取",
			R2Severity: "CRITICAL",
			R2Title:    "直接解引用argv[1]未检查argc",
		},
		{
			Name:       "Match 13 - BaseFrameWidget 构造函数未判空",
			R1Path:     "FRAME_UI/src/BaseFrameWidget.cpp",
			R1Line:     "15-30",
			R1Scope:    "FRAME_UI::BaseFrameWidget::BaseFrameWidget",
			R1Trigger:  "parent->layout()->addWidget(this);",
			R1Category: "空指针引用问题-未判空导致崩溃",
			R1Severity: "HIGH",
			R1Title:    "parent为空直接解引用",
			R2Path:     "FRAME_UI/src/BaseFrameWidget.cpp",
			R2Line:     "18-25",
			R2Scope:    "BaseFrameWidget::BaseFrameWidget", // 命名空间剥离
			R2Trigger:  "parent->layout()->addWidget(this);",
			R2Category: "防御性不足-空指针未检查",
			R2Severity: "HIGH",
			R2Title:    "parent指针及parent->layout()未判空",
		},
		{
			Name:       "Match 14 - InstanceApp IsRunning 线程竞争",
			R1Path:     "proj_core/src/InstanceApp.cpp",
			R1Line:     "88-92",
			R1Scope:    "PROJ_CORE_COMMON::InstanceApp::IsRunning",
			R1Trigger:  "return m_isRunning;",
			R1Category: "CWE-362: Concurrent Execution using Shared Resource with Improper Synchronization (Race Condition)",
			R1Severity: "HIGH",
			R1Title:    "非原子布尔变量并发读写",
			R2Path:     "proj_core/src/InstanceApp.cpp",
			R2Line:     "88-92",
			R2Scope:    "InstanceApp::IsRunning",
			R2Trigger:  "return m_isRunning;",
			R2Category: "并发问题-数据竞争",
			R2Severity: "HIGH",
			R2Title:    "多线程并发访问未加锁的m_isRunning标志",
		},
		{
			Name:       "Match 15 - MeasureDeviceManager 多函数声明规范化",
			R1Path:     "proj_meas/src/MeasureDeviceManager.cpp",
			R1Line:     "102-120",
			R1Scope:    "MeasureDeviceManager::GetTriggerDelay / GetTriggerFreq / GetDevTrigger",
			R1Trigger:  "return m_devTriggerMap[devId]->GetDelay();",
			R1Category: "CWE-476: NULL Pointer Dereference",
			R1Severity: "CRITICAL",
			R1Title:    "未检查设备是否存在直接取值",
			R2Path:     "proj_meas/src/MeasureDeviceManager.cpp",
			R2Line:     "105-118",
			R2Scope:    "MeasureDeviceManager::GetTriggerDelay / GetTriggerFreq", // 差一个函数
			R2Trigger:  "return m_devTriggerMap[devId]->GetDelay();",
			R2Category: "空指针解引用",
			R2Severity: "CRITICAL",
			R2Title:    "m_devTriggerMap未找到key时解引用空指针",
		},
		{
			Name:       "Match 16 - SignalHandlerRegister 回调 lambda 规范化",
			R1Path:     "proj_measurement/srv_shc/src/main.cpp",
			R1Line:     "45-55",
			R1Scope:    "main::operator()(signal handler)",
			R1Trigger:  "exit(signal);",
			R1Category: "CWE-364: Signal Handler Race Condition",
			R1Severity: "HIGH",
			R1Title:    "信号处理函数调用不可重入函数",
			R2Path:     "proj_measurement/srv_shc/src/main.cpp",
			R2Line:     "48-52",
			R2Scope:    "main::<lambda>", // 统一为 <lambda>
			R2Trigger:  "exit(signal);",
			R2Category: "并发与竞争-信号处理竞态",
			R2Severity: "HIGH",
			R2Title:    "信号处理器内非异步信号安全函数调用",
		},
		{
			Name:       "Match 17 - HighFreqImageUploadTask Encode 相邻触发行容错",
			R1Path:     "cam_yag/src/ImageUploadTask.cpp",
			R1Line:     "216-220",
			R1Scope:    "HighFreqImageUploadTask::Encode",
			R1Trigger:  "auto uploadImageData = ConvertImageDatasToUploadDatas(imageData);",
			R1Category: "内存管理问题-空指针解引用",
			R1Severity: "HIGH",
			R1Title:    "imageData未判空直接解引用",
			R2Path:     "cam_yag/src/ImageUploadTask.cpp",
			R2Line:     "218-222", // 行号相近
			R2Scope:    "HighFreqImageUploadTask::Encode",
			R2Trigger:  "m_encodeOutputs.emplace(imageData->timestamp(), uploadImageData);", // 同函数相邻行
			R2Category: "内存管理问题-空指针解引用",
			R2Severity: "HIGH",
			R2Title:    "imageData指针在emplace时被直接解引用导致崩溃",
		},
		{
			Name:       "Match 18 - DoubleBuffer SwapBuffer 死锁风险",
			R1Path:     "dev_util/src/DoubleBuffer.cpp",
			R1Line:     "80-95",
			R1Scope:    "DoubleBuffer::SwapBuffer",
			R1Trigger:  "m_mutex.lock();",
			R1Category: "死锁风险",
			R1Severity: "HIGH",
			R1Title:    "双锁顺序不一致引发死锁",
			R2Path:     "dev_util/src/DoubleBuffer.cpp",
			R2Line:     "82-93",
			R2Scope:    "DoubleBuffer::SwapBuffer",
			R2Trigger:  "m_mutex.lock();",
			R2Category: "并发问题-死锁风险",
			R2Severity: "HIGH",
			R2Title:    "双重加锁可能导致线程死锁",
		},
		{
			Name:       "Match 19 - RawDataTask ProcessBatch 空行号恢复",
			R1Path:     "proj_alignment/sap/src/RawDataTask.cpp",
			R1Line:     "120-135",
			R1Scope:    "RawDataTask::ProcessBatch",
			R1Trigger:  "m_buffer->push(batchData);",
			R1Category: "空指针解引用",
			R1Severity: "CRITICAL",
			R1Title:    "m_buffer判空缺失",
			R2Path:     "proj_alignment/sap/src/RawDataTask.cpp",
			R2Line:     "", // R2 行号为空！(现网 20 处空行号真实案例)
			R2Scope:    "RawDataTask::ProcessBatch",
			R2Trigger:  "m_buffer->push(batchData);",
			R2Category: "空指针解引用",
			R2Severity: "CRITICAL",
			R2Title:    "m_buffer指针为空解引用",
		},
		{
			Name:       "Match 20 - CameraDevice InitCamera 资源未释放",
			R1Path:     "cam_yag/src/CameraDevice.cpp",
			R1Line:     "310-330",
			R1Scope:    "CameraDevice::InitCamera",
			R1Trigger:  "if (!OpenDevice()) { return false; }",
			R1Category: "资源管理问题-内存泄漏",
			R1Severity: "MEDIUM",
			R1Title:    "提前退出导致句柄未关闭",
			R2Path:     "cam_yag/src/CameraDevice.cpp",
			R2Line:     "315-325",
			R2Scope:    "CameraDevice::InitCamera",
			R2Trigger:  "if (!OpenDevice()) { return false; }",
			R2Category: "资源或内存泄漏",
			R2Severity: "MEDIUM",
			R2Title:    "OpenDevice失败提前返回引发未初始化句柄泄漏",
		},
		{
			Name:       "Match 21 - StateConvertManager ConvertTo 空行号至有行号",
			R1Path:     "vmc/src/StateConvertManager.cpp",
			R1Line:     "", // R1 行号为空
			R1Scope:    "VMC::StateConvertManager::ConvertTo",
			R1Trigger:  "m_mutex.lock();",
			R1Category: "锁使用问题-资源未释放导致状态机挂起",
			R1Severity: "HIGH",
			R1Title:    "状态转换锁未释放",
			R2Path:     "vmc/src/StateConvertManager.cpp",
			R2Line:     "149-163", // R2 有行号
			R2Scope:    "StateConvertManager::ConvertTo",
			R2Trigger:  "m_mutex.lock();",
			R2Category: "逻辑错误-资源管理",
			R2Severity: "HIGH",
			R2Title:    "状态机转换加锁异常未解锁",
		},
		{
			Name:       "Match 22 - FocusProtocol ParseResponse 缓冲区溢出",
			R1Path:     "cam_yag/src/FocusProtocol.cpp",
			R1Line:     "180-195",
			R1Scope:    "FocusProtocol::ParseResponse",
			R1Trigger:  "memcpy(m_targetBuf, payload, length);",
			R1Category: "越界写",
			R1Severity: "CRITICAL",
			R1Title:    "memcpy长度未校验溢出",
			R2Path:     "cam_yag/src/FocusProtocol.cpp",
			R2Line:     "180-195",
			R2Scope:    "FocusProtocol::ParseResponse",
			R2Trigger:  "memcpy(m_targetBuf, payload, length);",
			R2Category: "内存管理问题-越界写",
			R2Severity: "CRITICAL",
			R2Title:    "length大于目标缓冲区导致缓冲区溢出崩溃",
		},
	}
}

func TestGoldenMatches22_AccuracyBenchmark(t *testing.T) {
	testDB := setupTestDB(t)
	if testDB == nil {
		return
	}
	oldDB := models.DB
	models.DB = testDB
	defer func() {
		models.DB = oldDB
	}()

	mustMigrate(t, models.DB,
		&models.Department{},
		&models.Repository{},
		&models.TaskReport{},
		&models.TaskType{},
		&models.User{},
		&models.DefectFingerprintRecord{},
	)

	repoID := uint(88888)
	taskTypeID := uint(99999)
	reportID1 := uint(10001)
	reportID2 := uint(10002)

	defer func() {
		models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Delete(&models.DefectFingerprintRecord{})
	}()

	cases := getGoldenTestCases22()
	totalCases := len(cases)
	if totalCases != 22 {
		t.Fatalf("Expected exactly 22 cases, got %d", totalCases)
	}

	// ── 步骤 1：全量构建 R1 扫描发现并入库 ──
	var r1Findings []models.AnalysisFinding
	var scannedFiles []string
	fileSet := make(map[string]bool)

	for _, c := range cases {
		r1Findings = append(r1Findings, models.AnalysisFinding{
			FilePath:    c.R1Path,
			LineNumber:  c.R1Line,
			TriggerLine: c.R1Trigger,
			ScopeSymbol: c.R1Scope,
			Category:    c.R1Category,
			Severity:    c.R1Severity,
			Title:       c.R1Title,
		})
		if !fileSet[c.R1Path] {
			scannedFiles = append(scannedFiles, c.R1Path)
			fileSet[c.R1Path] = true
		}
	}

	enrichedR1, err := DiffAndEnrichFindings(repoID, reportID1, taskTypeID, scannedFiles, r1Findings)
	if err != nil {
		t.Fatalf("R1 scan DiffAndEnrichFindings failed: %v", err)
	}

	// 检查 R1 全量入库均为 NEW
	for i, f := range enrichedR1 {
		if f.DiffStatus != models.DiffStatusNew {
			t.Errorf("R1 finding %d (%s) expected NEW, got %s", i+1, f.Title, f.DiffStatus)
		}
	}

	// ── 步骤 2：全量构建 R2 扫描输入并执行增量比对 ──
	var r2Findings []models.AnalysisFinding
	for _, c := range cases {
		r2Findings = append(r2Findings, models.AnalysisFinding{
			FilePath:    c.R2Path,
			LineNumber:  c.R2Line,
			TriggerLine: c.R2Trigger,
			ScopeSymbol: c.R2Scope,
			Category:    c.R2Category,
			Severity:    c.R2Severity,
			Title:       c.R2Title,
		})
	}

	enrichedR2, err := DiffAndEnrichFindings(repoID, reportID2, taskTypeID, scannedFiles, r2Findings)
	if err != nil {
		t.Fatalf("R2 scan DiffAndEnrichFindings failed: %v", err)
	}

	// ── 步骤 3：逐条核验 22 组案例对齐结果 ──
	matchedCount := 0
	failedList := []string{}

	for i, f := range enrichedR2 {
		c := cases[i]
		if f.DiffStatus == models.DiffStatusExisted {
			matchedCount++
			fmt.Printf("✅ [Aligned] Case %02d: %s -> EXISTED\n", i+1, c.Name)
		} else {
			failedList = append(failedList, fmt.Sprintf("❌ [Misaligned] Case %02d: %s -> got %s (expected EXISTED)", i+1, c.Name, f.DiffStatus))
		}
	}

	alignmentRate := float64(matchedCount) / float64(totalCases) * 100.0
	fmt.Printf("\n======================================================\n")
	fmt.Printf("🎯 [Golden Test Result] Aligned: %d/%d (%.2f%%)\n", matchedCount, totalCases, alignmentRate)
	fmt.Printf("======================================================\n")

	for _, msg := range failedList {
		fmt.Println(msg)
	}

	// 验收红线：对齐率必须 >= 95% (22 组中至少 21 组对齐)
	if alignmentRate < 95.0 {
		t.Fatalf("🚨 Alignment rate %.2f%% is below the 95%% threshold! Matched: %d/%d",
			alignmentRate, matchedCount, totalCases)
	}
}
