package orm

import (
	"context"
	"testing"
	"time"

	"github.com/openfoundry/lib/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm"
)

type Demo1 struct {
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

func (Demo1) TableName() string {
	return "demo1"
}

var demo1Data = []Demo1{
	{ID: "test1", StrField: "测试1", IntField: 1, Int64Field: 1, FloatField: 1, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试1", BlobField: []byte("测试1"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	{ID: "test2", StrField: "测试2", IntField: 2, Int64Field: 2, FloatField: 2, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试2", BlobField: []byte("测试2"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	{ID: "test3", StrField: "测试3", IntField: 3, Int64Field: 3, FloatField: 3, BoolField: true, TimeField: time.Now(), DateField: time.Now(), TextField: "测试3", BlobField: []byte("测试3"), JsonField: map[string]any{"key": "value"}, CreatedAt: time.Now(), UpdatedAt: time.Now()},
}

// 准备测试数据的辅助函数
func prepareDemo1(t *testing.T, db *xorm.Session) []Demo1 {
	for _, data := range demo1Data {
		_, err := db.Insert(&data)
		assert.NoError(t, err, "插入测试数据失败")
	}

	return demo1Data
}

func syncTestModel3(t *testing.T) {
	InitDB("sqlite3", ":memory:")
	m := MustLoadStructModel[Demo1]()
	err := m.Sync()
	require.NoError(t, err)
}

func TestPkg(t *testing.T) {
	syncTestModel3(t)

	session := MustSession(context.Background())
	defer session.Close()
}

func TestBooleanUpdate(t *testing.T) {
	var err error
	syncTestModel3(t)
	m := MustEntityOps[Demo1]()

	session := MustSession(context.Background())
	defer session.Close()
	prepareDemo1(t, session)

	model := m.WithSession(session)

	r, err := model.Get("test1")
	require.NoError(t, err)
	// println(util.MustPrettyJsonString(r))

	r.BoolField = false
	err = model.UpdateOne(r)
	assert.NoError(t, err)

	result, err := model.Get("test1")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, false, result.BoolField)

	println(util.MustPrettyJsonString(result))
}

func TestModel3Func(t *testing.T) {
	syncTestModel3(t)
	m := MustEntityOps[Demo1]()
	testData := demo1Data

	before := func() *xorm.Session {
		{
			session := MustSession(context.Background())
			defer session.Close()

			// 在测试开始前清理数据
			_, err := session.Exec("DELETE FROM demo1")
			assert.NoError(t, err, "清理初始数据失败")

			// 准备测试数据
			prepareDemo1(t, session)
		}
		return MustSession(context.Background())
	}

	t.Run("upsert", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)

		newData := Demo1{
			ID:       "test_upsert",
			StrField: "测试更新插入",
		}

		// 测试插入
		affected, err := model.Upsert(&newData)
		assert.NoError(t, err, "Upsert 操作失败")
		assert.Equal(t, int64(1), affected, "Upsert 应影响1行")

		// 验证插入结果
		result, err := model.Get(newData.ID)
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, newData.StrField, result.StrField, "插入的数据不匹配")

		// 测试更新
		newData.StrField = "测试更新"
		affected, err = model.Upsert(&newData)
		assert.NoError(t, err, "更新操作失败")
		assert.Equal(t, int64(1), affected, "更新应影响1行")

		// 验证更新后的结果
		updatedResult, err := model.Get(newData.ID)
		require.NoError(t, err, "获取更新后的数据失败")
		require.NotNil(t, updatedResult)
		assert.Equal(t, "测试更新", updatedResult.StrField, "更新后的数据不匹配")
	})

	t.Run("insert", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		newData := Demo1{
			ID:       "test_insert",
			StrField: "测试插入",
		}
		// 测试插入
		affected, err := model.Insert(&newData)
		assert.NoError(t, err, "Insert 操作失败")
		assert.Equal(t, int64(1), affected, "Insert 应影响1行")

		// 验证插入结果
		result, err := model.Get(newData.ID)
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, newData.StrField, result.StrField, "插入的数据不匹配")

		// 测试重复插入
		_, err = model.Insert(&newData)
		assert.Error(t, err, "重复插入应该失败")
	})

	t.Run("update", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)

		updateData := testData[0]
		updateData.StrField = "测试更新"

		affected, err := model.Update(&updateData)
		assert.NoError(t, err, "Update 操作失败")
		assert.Equal(t, int64(1), affected, "Update 应影响1行")

		// 验证更新后的结果
		result, err := model.Get(updateData.ID)
		require.NoError(t, err, "获取更新后的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, updateData.StrField, result.StrField, "更新后的数据不匹配")

		// 测试更新不存在的记录
		nonExistData := Demo1{
			ID:       "not_exist",
			StrField: "不存在的记录",
		}
		affected, err = model.Update(&nonExistData)
		assert.Error(t, err, "更新不存在的记录不应返回错误")
		assert.Equal(t, int64(0), affected, "更新不存在的记录应影响0行")
	})

	t.Run("updateIn", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		updateData := Demo1{
			StrField: "测试更新",
		}
		affected, err := model.UpdateIn(&updateData, []any{"test1", "test2", "test3"})
		assert.NoError(t, err, "UpdateIn 操作失败")
		assert.Equal(t, int64(3), affected, "UpdateIn 应影响3行")

		result, err := model.Get("test1")
		require.NoError(t, err, "获取更新后的数据失败")
		assert.Equal(t, updateData.StrField, result.StrField, "更新后的数据不匹配")

		result, err = model.Get("test2")
		require.NoError(t, err, "获取更新后的数据失败")
		assert.Equal(t, updateData.StrField, result.StrField, "更新后的数据不匹配")
	})

	t.Run("search", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		// 测试模糊搜索
		rows, err := model.Search(map[string]string{
			"where.id.like": "test",
		})
		assert.NoError(t, err, "搜索操作失败")
		assert.GreaterOrEqual(t, len(rows), 3, "搜索结果数量不正确")

		// 测试精确匹配
		rows, err = model.Search(map[string]string{
			"where.id.eq": "test1",
		})
		assert.NoError(t, err, "精确搜索失败")
		assert.Equal(t, 1, len(rows), "精确搜索应只返回一条记录")
		assert.Equal(t, "test1", rows[0].ID)
	})

	t.Run("count", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		// 测试模糊搜索
		count, err := model.Count(map[string]string{
			"where.id.like": "test",
		})
		assert.NoError(t, err, "搜索操作失败")
		assert.GreaterOrEqual(t, count, int64(3), "搜索结果数量不正确")

		// 测试精确匹配
		count, err = model.Count(map[string]string{
			"where.id.eq": "test1",
		})
		assert.NoError(t, err, "精确搜索失败")
		assert.Equal(t, int64(1), count, "精确搜索应只返回一条记录")
	})

	t.Run("searchPage", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		r, err := model.SearchPage(map[string]string{
			"where.id.like": "test",
			"page":          "0",
			"pagesize":      "2",
			"order":         "id.asc",
		})
		require.NoError(t, err, "分页搜索失败")
		assert.NotNil(t, r, "分页结果不应为空")
		assert.Equal(t, 2, len(r.Data), "分页大小不正确")
		assert.GreaterOrEqual(t, r.Total, int64(3), "总记录数不正确")
		assert.Equal(t, "test1", r.Data[0].ID)
	})

	t.Run("delete", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		// 删除第一条测试数据
		affected, err := model.Delete(testData[0].ID)
		assert.NoError(t, err, "删除操作失败")
		assert.Equal(t, int64(1), affected, "删除应影响1行")

		// 验证删除结果
		result, err := model.Get(testData[0].ID)
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")
	})

	t.Run("deleteIn", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		// 删除多条测试数据
		affected, err := model.DeleteIn([]any{testData[1].ID, testData[2].ID})
		assert.NoError(t, err, "删除操作失败")
		assert.Equal(t, int64(2), affected, "删除应影响2行")

		// 验证删除结果
		result, err := model.Get(testData[1].ID)
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")

		result, err = model.Get(testData[2].ID)
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")
	})

	t.Run("deleteWhere", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		affected, err := model.DeleteWhere(map[string]string{
			"where.id.like": "test",
		})
		assert.NoError(t, err, "删除操作失败")
		assert.Equal(t, int64(3), affected, "删除应影响3行")

		result, err := model.Get(demo1Data[1].ID)
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")
	})

	t.Run("insertOne", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		newData := Demo1{
			ID:       "test_insert_one",
			StrField: "测试单个插入",
		}

		// 测试插入
		err := model.InsertOne(&newData)
		assert.NoError(t, err, "InsertOne 操作失败")

		// 验证插入结果
		result, err := model.Get(newData.ID)
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, newData.StrField, result.StrField, "插入的数据不匹配")

		// 测试重复插入
		err = model.InsertOne(&newData)
		assert.Error(t, err, "重复插入应该失败")
	})

	t.Run("upsertOne", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		newData := Demo1{
			ID:       "test_upsert_one",
			StrField: "测试单个更新插入",
		}

		// 测试插入
		err := model.UpsertOne(&newData)
		assert.NoError(t, err, "UpsertOne 插入操作失败")

		// 验证插入结果
		result, err := model.Get(newData.ID)
		require.NoError(t, err, "获取插入的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, newData.StrField, result.StrField, "插入的数据不匹配")

		// 测试更新
		newData.StrField = "测试单个更新"
		err = model.UpsertOne(&newData)
		assert.NoError(t, err, "UpsertOne 更新操作失败")

		// 验证更新后的结果
		updatedResult, err := model.Get(newData.ID)
		require.NoError(t, err, "获取更新后的数据失败")
		require.NotNil(t, updatedResult)
		assert.Equal(t, "测试单个更新", updatedResult.StrField, "更新后的数据不匹配")
	})

	t.Run("updateOne", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		updateData := testData[0]
		updateData.StrField = "测试单个更新"

		err := model.UpdateOne(&updateData)
		assert.NoError(t, err, "UpdateOne 操作失败")

		// 验证更新后的结果
		result, err := model.Get(updateData.ID)
		require.NoError(t, err, "获取更新后的数据失败")
		require.NotNil(t, result)
		assert.Equal(t, updateData.StrField, result.StrField, "更新后的数据不匹配")

		// 测试更新不存在的记录
		nonExistData := Demo1{
			ID:       "not_exist",
			StrField: "不存在的记录",
		}
		err = model.UpdateOne(&nonExistData)
		assert.Error(t, err, "更新不存在的记录应该返回错误")
	})

	t.Run("deleteOne", func(t *testing.T) {
		session := before()
		defer session.Close()
		model := m.WithSession(session)
		// 删除第一条测试数据
		err := model.DeleteOne(testData[0].ID)
		assert.NoError(t, err, "DeleteOne 操作失败")

		// 验证删除结果
		result, err := model.Get(testData[0].ID)
		assert.NoError(t, err, "获取删除的数据失败")
		assert.Nil(t, result, "删除的数据应该不存在")

		// 测试删除不存在的记录, err != nil
		err = model.DeleteOne("not_exist")
		assert.Error(t, err, "删除不存在的记录应该返回错误")
	})
}
