### ChangeLog 2025-09-11

#### **`MapToStruct`** (增强)

- **[FIXED]** **支持时间解析**：现在能将 `"2025-09-11T11:22:24.281Z"` 格式的字符串正确转换为 `time.Time`。
- **[IMPROVED]** **更安全**：通过泛型强制要求传入指针，并优化了错误信息，便于调试。

#### **`StructToMap`** (重构)

- **[CHANGED]** **功能更完整**：改用 `encoding/json` 实现，以支持 `omitempty` 标签并正确处理 `time.Time` 到字符串的转换。
- **[FIXED]** **行为更可靠**：与 `MapToStruct` 行为对称，且对无效输入返回 `error` 而不是 `panic`。

#### **`StructToMapFast`** (新增)

- **[ADDED]** **保留高性能方案**：旧的 `structs` 库实现被重命名为此函数。
- **[USAGE]** 仅用于追求极致性能的内部场景，它不支持 `omitempty` 且 `time.Time` 不会转为字符串。
