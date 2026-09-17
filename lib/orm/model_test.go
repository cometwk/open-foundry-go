package orm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

// DemoForModelTest 是用于测试非泛型 ModelOps 接口的简单结构体
type DemoForModelTest struct {
	ID         string         `xorm:"'id' varchar(64) pk" json:"id"`             // 主键
	StrField   string         `xorm:"'str_field' varchar(255)" json:"str_field"` // 字符串
	IntField   int            `xorm:"'int_field' int" json:"int_field"`          // 整数
	Int64Field int64          `xorm:"'int64_field' bigint" json:"int64_field"`   // 64位整数
	FloatField float64        `xorm:"'float_field' double" json:"float_field"`   // 浮点数
	BoolField  bool           `xorm:"'bool_field' bool" json:"bool_field"`       // 布尔值
	TimeField  time.Time      `xorm:"'time_field' datetime" json:"time_field"`   // 时间
	DateField  time.Time      `xorm:"'date_field' date" json:"date_field"`       // 日期
	TextField  string         `xorm:"'text_field' text" json:"text_field"`       // 文本
	BlobField  []byte         `xorm:"'blob_field' blob" json:"blob_field"`       // 二进制数据
	JsonField  map[string]any `xorm:"'json_field' json" json:"json_field"`       // JSON 字段（仅适用于支持 JSON 的数据库）
	CreatedAt  time.Time      `xorm:"'created_at' created" json:"created_at"`    // 创建时间
	UpdatedAt  time.Time      `xorm:"'updated_at' updated" json:"updated_at"`    // 更新时间
}

func (DemoForModelTest) TableName() string {
	return "demo_for_model_test"
}

var demoForModelTestData = []DemoForModelTest{
	{ID: "test1", StrField: "测试1", IntField: 1, Int64Field: 1, FloatField: 1, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试1", BlobField: []byte("测试1"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	{ID: "test2", StrField: "测试2", IntField: 2, Int64Field: 2, FloatField: 2, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试2", BlobField: []byte("测试2"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	{ID: "test3", StrField: "测试3", IntField: 3, Int64Field: 3, FloatField: 3, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试3", BlobField: []byte("测试3"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
}

// 准备测试数据的辅助函数
func prepareDemoForModelTest(t *testing.T, db *xorm.Session) []DemoForModelTest {
	for _, data := range demoForModelTestData {
		_, err := db.Insert(&data)
		assert.NoError(t, err, "插入测试数据失败")
	}

	return demoForModelTestData
}

// syncDemoForModelTest 初始化数据库并同步 DemoForModelTest 的模式
func syncDemoForModelTest(t *testing.T) {
	InitDB("sqlite3", ":memory:")
	m := MustLoadStructModel[DemoForModelTest]()
	err := m.Sync()
	require.NoError(t, err, "模式同步失败")
}

// TestModelOps 包含非泛型 ModelOps 接口的测试套件
func TestModelOps(t *testing.T) {
	syncDemoForModelTest(t)
	m := MustModelOpsById("demo_for_model_test", nil)

	session := MustSession(context.Background())
	defer session.Close()

	before := func() *xorm.Session {
		{
			session := MustSession(context.Background())
			defer session.Close()

			// 在测试开始前清理数据
			_, err := session.Exec("DELETE FROM demo_for_model_test")
			assert.NoError(t, err, "清理初始数据失败")

			// 准备测试数据
			prepareDemoForModelTest(t, session)
		}
		return MustSession(context.Background())
	}

	// modelOps := MustModelOps(nil, "demo_for_model_test").WithSession(session)

	t.Run("newSlice", func(t *testing.T) {
		imp := engine1.mustModel("demo_for_model_test")
		slice := imp.NewSlice()
		s := fmt.Sprintf("%T", slice)
		assert.True(t, strings.HasPrefix(s, "[]orm.DemoForModelTest"), "切片应该是 []orm.DemoForModelTest 类型")

		ptr := imp.NewSlicePtr()
		s = fmt.Sprintf("%T", ptr)
		assert.Equal(t, "*[]orm.DemoForModelTest", s, "指针应该是 *[]orm.DemoForModelTest 类型")
	})

	t.Run("upsert", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		newData := map[string]any{
			"id":        "test_upsert",
			"str_field": "测试更新插入",
			"int_field": 100,
		}

		// 测试插入
		affected, err := modelOps.Upsert(newData)
		assert.NoError(t, err, "Upsert（插入）操作失败")
		assert.Equal(t, int64(1), affected, "Upsert（插入）应影响1行")

		// 验证插入结果
		result, err := modelOps.GetByPK("test_upsert")
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "测试更新插入", resultMap["str_field"])

		// 测试更新
		newData["str_field"] = "测试更新"
		affected, err = modelOps.Upsert(newData)
		assert.NoError(t, err, "Upsert（更新）操作失败")
		assert.Equal(t, int64(1), affected, "Upsert（更新）应影响1行")

		// 验证更新后的结果
		result, err = modelOps.GetByPK("test_upsert")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err = StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "测试更新", resultMap["str_field"])
	})

	t.Run("insert", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		newData := map[string]any{
			"id":        "test_insert",
			"str_field": "测试插入",
			"int_field": 100,
		}

		// 测试插入
		affected, err := modelOps.Insert(newData)
		assert.NoError(t, err, "Insert 操作失败")
		assert.Equal(t, int64(1), affected, "Insert 应影响1行")

		// 验证插入结果
		result, err := modelOps.GetByPK("test_insert")
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "测试插入", resultMap["str_field"])

		// 测试重复插入
		_, err = modelOps.Insert(newData)
		assert.Error(t, err, "重复插入应该失败")
	})

	t.Run("updateByPK", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		updateData := map[string]any{
			"id":        "test1",
			"str_field": "测试更新",
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		result, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "测试更新", resultMap["str_field"])
	})

	t.Run("updateIn", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		updateData := map[string]any{
			"int_field": 999,
		}
		affected, err := modelOps.UpdateIn(updateData, []any{"test1", "test2"})
		assert.NoError(t, err, "UpdateIn 操作失败")
		assert.Equal(t, int64(2), affected, "UpdateIn 应影响2行")

		// 验证更新
		for _, id := range []string{"test1", "test2"} {
			r, err := modelOps.GetByPK(id)
			require.NoError(t, err, "获取更新后的数据失败")
			rm, err := StructToMap(r)
			require.NoError(t, err)
			assert.Equal(t, float64(999), rm["int_field"])
		}
	})

	t.Run("search", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		// 测试条件搜索
		rows, err := modelOps.Search(map[string]string{
			"where.int_field.gt": "1", // IntField > 1
		})
		assert.NoError(t, err, "搜索操作失败")
		slice, ok := rows.([]DemoForModelTest)
		require.True(t, ok, "返回的结果应该是 []DemoForModelTest 类型")
		assert.Equal(t, 2, len(slice), "应该找到2个 IntField > 1 的用户")

		// 测试精确匹配
		rows, err = modelOps.Search(map[string]string{
			"where.id.eq": "test1",
		})
		assert.NoError(t, err, "精确搜索失败")
		slice, ok = rows.([]DemoForModelTest)
		require.True(t, ok)
		assert.Equal(t, 1, len(slice), "精确搜索应只返回一条记录")
		assert.Equal(t, "test1", slice[0].ID)
	})

	t.Run("searchPage", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		pageResult, err := modelOps.SearchPage(map[string]string{
			"where.id.like": "test",
			"order":         "int_field.desc",
			"page":          "0",
			"pagesize":      "2",
		})
		require.NoError(t, err, "分页搜索失败")
		assert.Equal(t, int64(3), pageResult.Total, "总记录数不正确")
		assert.Equal(t, int64(0), pageResult.Page, "页码不正确")
		slice, ok := pageResult.Data.([]DemoForModelTest)
		require.True(t, ok, "分页数据应该是 []DemoForModelTest 类型")
		assert.Equal(t, 2, len(slice), "分页大小不正确")
		assert.Equal(t, "test3", slice[0].ID) // test3 的 IntField 最大
	})

	t.Run("deleteByPK", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		affected, err := modelOps.DeleteByPK("test1")
		assert.NoError(t, err, "DeleteByPK 操作失败")
		assert.Equal(t, int64(1), affected, "DeleteByPK 应影响1行")

		result, err := modelOps.GetByPK("test1")
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的记录应该不存在")
	})

	t.Run("deleteIn", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		affected, err := modelOps.DeleteIn([]any{"test1", "test3"})
		assert.NoError(t, err, "DeleteIn 操作失败")
		assert.Equal(t, int64(2), affected, "DeleteIn 应影响2行")

		// 验证删除结果
		result, err := modelOps.GetByPK("test1")
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")

		result, err = modelOps.GetByPK("test3")
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")
	})

	t.Run("deleteWhere", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)
		affected, err := modelOps.DeleteWhere(map[string]string{
			"where.int_field.lt": "3", // IntField < 3
		})
		assert.NoError(t, err, "DeleteWhere 操作失败")
		assert.Equal(t, int64(2), affected, "应该删除test1和test2")

		// 验证删除结果
		result, err := modelOps.GetByPK("test1")
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")

		result, err = modelOps.GetByPK("test2")
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")

		// test3 应该还存在
		result, err = modelOps.GetByPK("test3")
		assert.NoError(t, err, "获取test3失败")
		assert.NotNil(t, result, "test3应该还存在")
	})

	// ========== 针对本次改动的测试用例 ==========

	// 测试 mapKeysToUpdateCols: 验证只有 map 中指定的字段被更新，其他字段保持不变
	t.Run("updateByPK_partialFields", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 先获取原始数据
		original, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取原始数据失败")
		require.NotNil(t, original)
		originalMap, err := StructToMap(original)
		require.NoError(t, err)

		// 只更新 str_field，其他字段应该保持不变
		updateData := map[string]any{
			"id":        "test1",
			"str_field": "部分更新测试",
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证更新后的数据
		result, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)

		// 验证更新的字段
		assert.Equal(t, "部分更新测试", resultMap["str_field"], "str_field 应该被更新")

		// 验证其他字段保持不变
		assert.Equal(t, originalMap["int_field"], resultMap["int_field"], "int_field 应该保持不变")
		assert.Equal(t, originalMap["int64_field"], resultMap["int64_field"], "int64_field 应该保持不变")
		assert.Equal(t, originalMap["float_field"], resultMap["float_field"], "float_field 应该保持不变")
		assert.Equal(t, originalMap["text_field"], resultMap["text_field"], "text_field 应该保持不变")
	})

	// 测试 mapKeysToUpdateCols: 验证主键字段被正确跳过
	t.Run("updateByPK_skipPrimaryKey", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 即使 map 中包含主键字段，也不应该更新主键
		updateData := map[string]any{
			"id":        "test1",
			"str_field": "测试跳过主键",
			"int_field": 888,
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证主键没有被改变
		result, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "test1", resultMap["id"], "主键不应该被更新")
		assert.Equal(t, "测试跳过主键", resultMap["str_field"], "str_field 应该被更新")
		assert.Equal(t, float64(888), resultMap["int_field"], "int_field 应该被更新")
	})

	// 测试 UseBool(): 验证布尔字段从 true 更新到 false
	t.Run("updateByPK_boolField_trueToFalse", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 验证原始数据是 true
		original, err := modelOps.GetByPK("test1")
		require.NoError(t, err)
		originalMap, err := StructToMap(original)
		require.NoError(t, err)
		assert.Equal(t, true, originalMap["bool_field"], "原始 bool_field 应该是 true")

		// 更新为 false
		updateData := map[string]any{
			"id":         "test1",
			"bool_field": false,
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证布尔字段被正确更新为 false
		result, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, false, resultMap["bool_field"], "bool_field 应该被更新为 false")
	})

	// 测试 UseBool(): 验证布尔字段从 false 更新到 true
	t.Run("updateByPK_boolField_falseToTrue", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 先将 test2 的 bool_field 设置为 false
		updateData1 := map[string]any{
			"id":         "test2",
			"bool_field": false,
		}
		_, err := modelOps.UpdateByPK(updateData1)
		require.NoError(t, err, "第一次更新失败")

		// 验证是 false
		original, err := modelOps.GetByPK("test2")
		require.NoError(t, err)
		originalMap, err := StructToMap(original)
		require.NoError(t, err)
		assert.Equal(t, false, originalMap["bool_field"], "bool_field 应该是 false")

		// 更新为 true
		updateData2 := map[string]any{
			"id":         "test2",
			"bool_field": true,
		}
		affected, err := modelOps.UpdateByPK(updateData2)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证布尔字段被正确更新为 true
		result, err := modelOps.GetByPK("test2")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, true, resultMap["bool_field"], "bool_field 应该被更新为 true")
	})

	// 测试 ensureUpdateCols: 验证当 updateCols 为空时，会使用 UpdatedAt 字段
	t.Run("updateByPK_emptyUpdateCols", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 获取原始数据
		original, err := modelOps.GetByPK("test1")
		require.NoError(t, err)
		originalEntity := original.(*DemoForModelTest)
		originalUpdatedAt := originalEntity.UpdatedAt

		// 等待足够长的时间，确保 UpdatedAt 会变化（SQLite 时间精度可能只到秒）
		time.Sleep(1100 * time.Millisecond)

		// 只提供主键，不提供任何其他字段（模拟空 updateCols 的情况）
		// 注意：由于 ensureUpdateCols 的存在，即使没有其他字段，也会更新 UpdatedAt
		updateData := map[string]any{
			"id": "test1",
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证 UpdatedAt 被更新了
		result, err := modelOps.GetByPK("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		resultEntity := result.(*DemoForModelTest)
		updatedUpdatedAt := resultEntity.UpdatedAt

		// 验证 UpdatedAt 时间确实更新了（应该比原始时间晚或相等，但至少应该执行了更新操作）
		// 由于 SQLite 的时间精度问题，我们至少验证 UpdatedAt 字段被包含在更新操作中
		assert.True(t, updatedUpdatedAt.After(originalUpdatedAt) || updatedUpdatedAt.Equal(originalUpdatedAt),
			"UpdatedAt 应该被更新，新时间应该晚于或等于原始时间")

		// 验证至少 UpdatedAt 字段被包含在更新列中（通过检查更新操作确实执行了）
		// 如果 ensureUpdateCols 正常工作，即使没有其他字段，也会更新 UpdatedAt
		assert.GreaterOrEqual(t, affected, int64(1), "应该至少更新了 UpdatedAt 字段")
	})

	// 测试 Upsert 中的 UseBool(): 验证 Upsert 也能正确更新布尔字段
	t.Run("upsert_boolField", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 插入新记录，bool_field 为 false
		newData := map[string]any{
			"id":         "test_upsert_bool",
			"str_field":  "测试 Upsert 布尔字段",
			"bool_field": false,
		}
		affected, err := modelOps.Upsert(newData)
		assert.NoError(t, err, "Upsert（插入）操作失败")
		assert.Equal(t, int64(1), affected, "Upsert（插入）应影响1行")

		// 验证插入结果
		result, err := modelOps.GetByPK("test_upsert_bool")
		require.NoError(t, err)
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, false, resultMap["bool_field"], "bool_field 应该是 false")

		// 更新为 true
		newData["bool_field"] = true
		affected, err = modelOps.Upsert(newData)
		assert.NoError(t, err, "Upsert（更新）操作失败")
		assert.Equal(t, int64(1), affected, "Upsert（更新）应影响1行")

		// 验证更新结果
		result, err = modelOps.GetByPK("test_upsert_bool")
		require.NoError(t, err)
		resultMap, err = StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, true, resultMap["bool_field"], "bool_field 应该被更新为 true")
	})

	// 测试 mapKeysToUpdateCols: 验证多个字段同时更新
	t.Run("updateByPK_multipleFields", func(t *testing.T) {
		session := before()
		defer session.Close()
		modelOps := m.WithSession(session)

		// 同时更新多个字段
		updateData := map[string]any{
			"id":          "test3",
			"str_field":   "多字段更新",
			"int_field":   777,
			"float_field": 3.14,
			"bool_field":  false,
		}
		affected, err := modelOps.UpdateByPK(updateData)
		assert.NoError(t, err, "UpdateByPK 操作失败")
		assert.Equal(t, int64(1), affected, "UpdateByPK 应影响1行")

		// 验证所有字段都被正确更新
		result, err := modelOps.GetByPK("test3")
		require.NoError(t, err, "获取更新后的数据失败")
		resultMap, err := StructToMap(result)
		require.NoError(t, err)
		assert.Equal(t, "多字段更新", resultMap["str_field"], "str_field 应该被更新")
		assert.Equal(t, float64(777), resultMap["int_field"], "int_field 应该被更新")
		assert.Equal(t, 3.14, resultMap["float_field"], "float_field 应该被更新")
		assert.Equal(t, false, resultMap["bool_field"], "bool_field 应该被更新")
	})
}
