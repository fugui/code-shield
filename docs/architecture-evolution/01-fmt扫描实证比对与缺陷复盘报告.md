# Code-Shield 代码扫描实证比对与缺陷复盘报告

> **目标仓库**：`scylladb/fmt`（现代化 C++ 格式化库，`repo_id: 167`）  
> **任务类型**：`coredump_risk`（崩溃与内存安全风险扫描）  
> **分析样本**：`report-147-synthesis-fmt.json` vs `report-150-synthesis-fmt.json`  
> **验证工具**：GCC 13.3 (C++17)、AddressSanitizer (ASan)、UndefinedBehaviorSanitizer (UBSan)、GDB

---

## 一、 实证比对背景与宏观指标

在 Code-Shield 对同一个代码仓 `scylladb/fmt` 执行的两次不同扫描任务中，生成了两份综合报告：`report-147` 与 `report-150`。通过比对发现，两次扫描结果在**缺陷检出范围、关注视角、严重等级评估**上呈现出显著的差异性和互补性。

### 1.1 宏观指标对比表

| 评估维度 | 报告 147 (`report-147`) | 报告 150 (`report-150`) | 差异与结论 |
| :--- | :--- | :--- | :--- |
| **检出问题总数** | **4 项** | **5 项** | 报告 150 多检出 1 项 |
| **严重等级分布** | 严重 1 项 / 一般 3 项 / 建议 0 项 | 严重 1 项 / 一般 3 项 / 建议 1 项 | 两者均包含 1 项严重级缺陷 |
| **覆盖核心文件** | `src/posix.cc`<br>`include/fmt/core.h`<br>`include/fmt/format.h`<br>`include/fmt/format-inl.h` | `src/posix.cc`<br>`include/fmt/posix.h`<br>`include/fmt/format.h`<br>`include/fmt/format-inl.h` | 报告 147 覆盖了 `core.h`，报告 150 覆盖了 `posix.h` |
| **主要缺陷分类** | - 内存管理-空指针解引用 (2 项)<br>- 其它问题-其它崩溃隐患 (2 项) | - 内存管理-越界访问/缓冲区溢出 (3 项)<br>- 内存管理-空指针解引用 (2 项) | **报告 150 发现了更具破坏性的内存越界与栈破坏**，报告 147 侧重于 DoS/OOM |
| **分析视角倾向** | 关注**外部调用契约与入参防御**（如 `nullptr` 格式串、超大宽度/精度申请） | 深入**底层算法状态机与内存模型**（如 Grisu2 浮点栈溢出、词法扫描越界） | 两者视角互补，单一报告均存在视野盲区 |

---

## 二、 全量 8 大问题源码核实与实测结果

经过对原始代码仓 `/home/fugui/codes/code-shield/codes/scylladb/fmt` 逐行比对并构建独立 C++ PoC 测试，全量 8 个问题的核实结果如下：

### 2.1 全量问题映射矩阵

| # | 缺陷点位（文件:行号） | 缺陷简述 | 报告 147 | 报告 150 | 动态 PoC 实测表现 | 准确性 | 修复必要性 |
| :---: | :--- | :--- | :---: | :---: | :--- | :---: | :---: |
| **1** | `src/posix.cc:93-98` | `buffered_file::fileno()` 空指针解引用 | ✅ (一般) | ✅ (一般) | **SIGSEGV 段错误 (Core dumped)** | 100% 准确 | **高 (High)** |
| **2** | `include/fmt/core.h:378-379` | `basic_string_view(nullptr)` 触发 `strlen` 崩溃 | ✅ (严重) | ❌ | **SIGSEGV 段错误 (Core dumped)** | 100% 准确 | **中~高 (Medium-High)** |
| **3** | `include/fmt/format.h:2738-2744` | 格式化宽度（`width`）无上限导致 OOM | ✅ (一般) | ❌ | **单次堆申请 2GB+ 内存** | 100% 准确 | **中 (Medium)** |
| **4** | `include/fmt/format-inl.h:746-755` | 浮点精度（`precision`）无上限导致 OOM | ✅ (一般) | ❌ | **单次堆申请 1GB+ 内存** | 100% 准确 | **中 (Medium)** |
| **5** | `include/fmt/format-inl.h:611-654` | Grisu2 浮点格式化栈缓冲越界写 | ❌ | ✅ (严重) | **ASan `stack-buffer-overflow` (写越界 798 字节)** | 100% 准确 (需启用宏) | **极高 (Critical)** |
| **6** | `include/fmt/format-inl.h:522-527` | Grisu2 幂表 `POWERS_OF_10_32` 索引越界读 | ❌ | ✅ (一般) | **UBSan `index 13 out of bounds`** | 100% 准确 (需启用宏) | **高 (High)** |
| **7** | `include/fmt/format.h:1915-1919` | `parse_arg_id` 先自增后解引用堆越界读 | ❌ | ✅ (一般) | **ASan `heap-buffer-overflow` (越界读 1 字节)** | 100% 准确 (默认可达) | **极高 (Critical)** |
| **8** | `include/fmt/posix.h:172-174` | `buffered_file::vprint` 传 NULL 给 `fwrite` 崩溃 | ❌ | ✅ (建议) | **SIGSEGV 段错误 (Core dumped)** | 100% 准确 | **高 (High)** |

---

## 三、 缺陷深度剖析与PoC验证详情

### 1. `buffered_file::fileno()` 空指针解引用 (147-#1 & 150-#1)
*   **代码片段**：
    ```cpp
    int buffered_file::fileno() const {
      int fd = FMT_POSIX_CALL(fileno FMT_ARGS(file_));
      if (fd == -1)
        FMT_THROW(system_error(errno, "cannot get file descriptor"));
      return fd;
    }
    ```
*   **根因分析**：`buffered_file` 允许默认构造、被 `std::move` 移动或显式调用 `close()`，此时内部指针 `file_ == FMT_NULL`。调用 `fileno()` 时，底层 glibc `fileno(NULL)` 直接读取 `((FILE*)NULL)->_fileno` 触发段错误，无法依赖返回值 `-1` 抛出异常。
*   **PoC 实测**：调用 `fmt::buffered_file bf; bf.fileno();`，进程立即崩溃，生成 Core dump。

---

### 2. `basic_string_view` 空指针触发 `strlen` 段错误 (147-#2)
*   **代码片段**：
    ```cpp
    FMT_CONSTEXPR basic_string_view(const Char *s)
      : data_(s), size_(internal::length(s)) {}
    ```
*   **根因分析**：`internal::length(s)` 在 GCC 下直接内联为 `std::strlen(s)`。若业务在打印日志时将可能为空的字符串指针（如 `const char* msg = nullptr`）直接传给 `fmt::format(msg, ...)`，将在构造 `string_view` 阶段引发未捕获的 SIGSEGV。
*   **PoC 实测**：`fmt::string_view sv((const char*)nullptr);` 触发段错误退出。

---

### 3. 格式化宽度（`width`）无上限导致 OOM (147-#3)
*   **代码片段**：
    ```cpp
    template <typename Range>
    template <typename F>
    void basic_writer<Range>::write_padded(const align_spec &spec, F &&f) {
      unsigned width = spec.width();
      ...
      auto &&it = reserve(width + (size - num_code_points));
    ```
*   **根因分析**：`spec.width()` 仅校验 `<= INT_MAX`。恶意或异常格式串（如 `fmt::format("{:2000000000}", "x")`）会迫使底层缓冲直接分配 2GB+ 内存。在受限容器或 32 位环境下将触发 `std::bad_alloc` 未捕获崩溃（SIGABRT）或被内核 OOM Killer 强杀。
*   **PoC 实测**：成功申请 2,000,000,000 字节内存并持续消耗 CPU 填充空格。

---

### 4. 浮点精度（`precision`）无上限导致 OOM (147-#4)
*   **代码片段**：
    ```cpp
    int result = internal::char_traits<char>::format_float(
        start, buffer_size, format, spec.precision, value);
    if (result >= 0) {
      unsigned n = internal::to_unsigned(result);
      buffer.reserve(n + 1);
    }
    ```
*   **根因分析**：`sprintf_format` 探测浮点输出长度，当使用超大精度（如 `{:.1000000000f}`）时，`snprintf` 返回极大数值，随后原样调用 `buffer.reserve(n + 1)` 申请超大堆内存。

---

### 5. Grisu2 浮点格式化栈缓冲越界写 (150-#2)
*   **代码片段**：
    ```cpp
    // include/fmt/format-inl.h:652
    std::uninitialized_fill_n(buffer + size, num_zeros, '0');
    ```
*   **根因分析**：`basic_memory_buffer` 默认内联栈空间为 500 字节。当启用 `FMT_USE_GRISU=1` 且浮点精度 > 500（如 `{:.800f}`）时，`format_float` 直接向 `buffer.data()` 写入字符而**未预先扩容**，导致在 `write_double` 栈帧上越界写入 798 字节。此外，`size == 0` 时第 611 行 `memmove(..., size - 1)` 的 `size - 1` 下溢为 `SIZE_MAX`。
*   **PoC 实测**：AddressSanitizer 精准拦截并报错：
    ```
    SUMMARY: AddressSanitizer: stack-buffer-overflow in memset
    Memory access at offset 856 overflows variable 'buffer' in write_double
    ```

---

### 6. Grisu2 幂表 `POWERS_OF_10_32` 索引越界读 (150-#3)
*   **代码片段**：
    ```cpp
    // include/fmt/format-inl.h:522-527
    lo &= one.f - 1;
    --exp;
    if (lo < delta || size > max_digits) {
      return grisu2_round(buffer, size, max_digits, delta, lo, one.f,
                          diff.f * data::POWERS_OF_10_32[-exp], exp);
    }
    ```
*   **根因分析**：`POWERS_OF_10_32` 仅定义了 10 个元素。但在循环中 `--exp` 随循环迭代不断自减（`-exp` 持续自增），无边界防护，导致越界读取静态全局内存。
*   **PoC 实测**：UBSan 精准拦截并报警：
    ```
    runtime error: index 13 out of bounds for type 'unsigned int [10]'
    ```

---

### 7. `parse_arg_id` 先自增后解引用导致堆越界读 (150-#4)
*   **代码片段**：
    ```cpp
    // include/fmt/format.h:1915-1919
    auto it = begin;
    do {
      c = *++it;
    } while (it != end && (is_name_start(c) || ('0' <= c && c <= '9')));
    handler(basic_string_view<Char>(begin, to_unsigned(it - begin)));
    ```
*   **根因分析**：经典词法扫描状态机缺陷——**先递增指针并读取，后检查是否越过 `end` 边界**。当格式串在命名参数处截断且末尾无 `\0`（如 `string_view` 截断视图 `"{a"`）时，`++it` 直接越过 `end` 越界读取 1 字节堆内存。在页边界处会直接产生段错误。
*   **PoC 实测**：AddressSanitizer 精准捕获堆越界读取：
    ```
    SUMMARY: AddressSanitizer: heap-buffer-overflow in parse_arg_id
    READ of size 1 at 0x502000000012
    ```

---

### 8. `buffered_file::vprint` 空指针传给 `fwrite` 崩溃 (150-#5)
*   **代码片段**：
    ```cpp
    void vprint(string_view format_str, format_args args) {
      fmt::vprint(file_, format_str, args);
    }
    ```
*   **根因分析**：默认构造的 `buffered_file` 内部 `file_ == nullptr`。调用 `vprint` 或 `print` 会把 `nullptr` 透传给底层的 `fmt::vprint(FILE*, ...)`，后者直接执行 `std::fwrite(data, 1, size, f)`，导致 glibc 解引用 NULL 指针崩溃。
*   **PoC 实测**：进程立即 SIGSEGV 崩溃。

---

## 四、 核心启示与架构痛点总结

从本次对 `fmt` 的实证复盘中，我们得出以下关键启示：

1. **单模型扫描具有显著的局限性**：无论是哪个前沿模型，在单次扫描中都无法兼顾“API 契约边界”、“资源消耗防御”与“底层算法状态机”。
2. **静态扫描必须引入“实证闭环”**：类似 Grisu2 栈溢出与词法解析器堆越界读，必须能够自动生成微型测试用例并在沙箱中通过 ASan/UBSan 验证，才能赋予报告极高的工业级公信力。
3. **严重度评级亟需标准化体系**：不能依赖大模型的自由发挥，必须建立基于 CWE 类别、触发前提条件、可达性与破坏后果的确定性定级规则。
