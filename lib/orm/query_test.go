package orm_test

// import (
// 	"testing"
// 	"time"

// 	"github.com/lucky-byte/reactgo/lib/pkg/orm"
// 	"github.com/lucky-byte/reactgo/lib/pkg/testutil"
// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/require"
// 	"xorm.io/builder"
// 	"xorm.io/xorm"
// )

// type TestModel struct {
// 	ID        int       `xorm:"'id' pk autoincr" json:"id"`
// 	Name      string    `xorm:"'name'" json:"name"`
// 	Age       int       `xorm:"'age'" json:"age"`
// 	IsEnabled bool      `xorm:"'is_enabled'" json:"is_enabled"`
// 	CreatedAt time.Time `xorm:"'created_at' created" json:"created_at"`
// }

// type TestAgeModel struct {
// 	Age     int    `xorm:"'age' pk" json:"age"`
// 	AgeDesc string `xorm:"'age_desc'" json:"age_desc"`
// }

// func initTestModel(t *testing.T) (*xorm.Engine, testutil.Logmem) {
// 	engine, err := xorm.NewEngine("sqlite3", ":memory:")
// 	require.NoError(t, err)
// 	engine.Sync2(new(TestModel))
// 	engine.Sync2(new(TestAgeModel))

// 	log := testutil.NewLogger()
// 	engine.SetLogger(log)
// 	// 全部采用 UTC 时区
// 	engine.TZLocation = time.UTC // 应用时使用 UTC
// 	engine.DatabaseTZ = time.UTC // 数据库存储时使用 UTC

// 	return engine, log
// }

// func TestTemplateQuery(t *testing.T) {
// 	db, log := initTestModel(t)
// 	session := db.NewSession()
// 	defer session.Close()

// 	// 开始记录日志
// 	log.Reset()

// 	result := make([]TestModel, 0)
// 	err := session.Where(builder.Eq{"age": 25}).Where(builder.Eq{"name": "test"}).Find(&result)
// 	require.NoError(t, err)

// 	expected := []testutil.LogEntry{
// 		{
// 			SQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE age=? AND name=?",
// 			Args: []any{25, "test"},
// 		},
// 	}
// 	assert.Equal(t, expected, log.Entries())
// }

// func TestBindQueryString(t *testing.T) {
// 	// Define a fixed local time zone for testing purposes
// 	localLocation, err := time.LoadLocation("Asia/Shanghai") // Or any other fixed local timezone
// 	require.NoError(t, err, "Failed to load local timezone")

// 	testCases := []struct {
// 		name         string
// 		params       map[string]string
// 		expectedSQL  string
// 		expectedArgs []any
// 		noExec       bool // 对于无法在 TestModel 上执行的查询，设置为 true
// 	}{
// 		{
// 			name: "测试等于查询",
// 			params: map[string]string{
// 				"where.age.eq":  "25",
// 				"where.name.eq": "test",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age`=? AND `name`=?",
// 			expectedArgs: []any{"25", "test"},
// 		},
// 		{
// 			name: "测试大于查询",
// 			params: map[string]string{
// 				"where.age.gt": "30",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age`>?",
// 			expectedArgs: []any{"30"},
// 		},
// 		{
// 			name: "测试多条件查询",
// 			params: map[string]string{
// 				"where.age.gte":       "25",
// 				"where.is_enabled.eq": "true",
// 				"order":               "age.desc,name",
// 				"select":              "name,age",
// 			},
// 			expectedSQL:  "SELECT `name`, `age` FROM `test_model` WHERE `age`>=? AND `is_enabled`=? ORDER BY `age` desc, `name` asc",
// 			expectedArgs: []any{"25", "true"},
// 		},
// 		{
// 			name: "测试全局模糊搜索",
// 			params: map[string]string{
// 				"q.name.age": "searchterm",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE (`name` LIKE ? OR `age` LIKE ?)",
// 			expectedArgs: []any{"%searchterm%", "%searchterm%"},
// 		},
// 		{
// 			name: "测试指定字段模糊搜索",
// 			params: map[string]string{
// 				"q.name.id": "a234",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE (`name` LIKE ? OR `id` LIKE ?)",
// 			expectedArgs: []any{"%a234%", "%a234%"},
// 		},
// 		{
// 			name: "测试 IN 查询",
// 			params: map[string]string{
// 				"where.age.in": "25,30,35",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age` IN (?,?,?)",
// 			expectedArgs: []any{"25", "30", "35"},
// 		},
// 		{
// 			name: "测试 LIKE 查询",
// 			params: map[string]string{
// 				"where.name.like": "%test%",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `name` LIKE ?",
// 			expectedArgs: []any{"%test%"},
// 		},
// 		{
// 			name: "测试 BETWEEN 查询",
// 			params: map[string]string{
// 				"where.age.btw": "20,30",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age` BETWEEN ? AND ?",
// 			expectedArgs: []any{"20", "30"},
// 		},
// 		{
// 			name: "测试 IS NULL 查询",
// 			params: map[string]string{
// 				"where.name.null": "true",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `name` IS NULL",
// 			expectedArgs: []any{},
// 		},
// 		{
// 			name: "测试 IS NOT NULL 查询",
// 			params: map[string]string{
// 				"where.name.null": "false",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `name` IS NOT NULL",
// 			expectedArgs: []any{},
// 		},
// 		{
// 			name: "测试 notIn 查询",
// 			params: map[string]string{
// 				"where.age.notIn": "40,50",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age` NOT IN (?,?)",
// 			expectedArgs: []any{"40", "50"},
// 		},
// 		{
// 			name: "测试 likes 查询",
// 			params: map[string]string{
// 				"where.name.likes": "test,user",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `name` LIKE ? AND `name` LIKE ?",
// 			expectedArgs: []any{"%test%", "%user%"},
// 		},
// 		{
// 			name:        "测试 time 查询 (本地时间转UTC)",
// 			params:      map[string]string{"where.created_at.time": "2024-01-01 00:00:00,2024-01-31 23:59:59"},
// 			expectedSQL: "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `created_at` BETWEEN ? AND ?",
// 			expectedArgs: []any{
// 				time.Date(2024, 1, 1, 0, 0, 0, 0, localLocation).UTC(),
// 				time.Date(2024, 1, 31, 23, 59, 59, 0, localLocation).UTC(),
// 			},
// 			noExec: false, // Now it can be executed
// 		},
// 		{
// 			name: "测试 neq, lt, lte 查询",
// 			params: map[string]string{
// 				"where.name.neq": "test",
// 				"where.age.lt":   "50",
// 				"where.id.lte":   "100",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age`<? AND `id`<=? AND `name`<>?",
// 			expectedArgs: []any{"50", "100", "test"},
// 		},
// 		{
// 			name: "测试 order asc 查询",
// 			params: map[string]string{
// 				"where.name.neq": "test",
// 				"where.age.lt":   "50",
// 				"where.id.lte":   "100",
// 				"order":          "name.asc",
// 			},
// 			expectedSQL:  "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age`<? AND `id`<=? AND `name`<>? ORDER BY `name` asc",
// 			expectedArgs: []any{"50", "100", "test"},
// 		},
// 	}

// 	for _, tc := range testCases {
// 		t.Run(tc.name, func(t *testing.T) {
// 			db, log := initTestModel(t)
// 			session := db.NewSession()
// 			defer session.Close()

// 			err := orm.BindQueryString(session, tc.params)
// 			require.NoError(t, err)

// 			if tc.noExec {
// 				// 对于无法在 TestModel 上执行的查询，只检查生成的 SQL
// 				sql, args := session.LastSQL()
// 				expectedQuery, err := builder.ConvertToBoundSQL(tc.expectedSQL, tc.expectedArgs)
// 				require.NoError(t, err)
// 				loggedQuery, err := builder.ConvertToBoundSQL(sql, args)
// 				require.NoError(t, err)
// 				assert.Equal(t, expectedQuery, loggedQuery)
// 				return
// 			}

// 			log.Reset()
// 			var result []TestModel
// 			err = session.Find(&result)
// 			require.NoError(t, err)

// 			require.NotEmpty(t, log.Entries(), "未记录到任何 SQL 查询")
// 			logged := log.Entries()[0]

// 			expectedQuery, err := builder.ConvertToBoundSQL(tc.expectedSQL, tc.expectedArgs)
// 			require.NoError(t, err)

// 			loggedQuery, err := builder.ConvertToBoundSQL(logged.SQL, logged.Args)
// 			require.NoError(t, err)

// 			assert.Equal(t, expectedQuery, loggedQuery)
// 		})
// 	}
// }

// func TestBindQueryStringWithPage(t *testing.T) {
// 	db, log := initTestModel(t)
// 	session := db.NewSession()
// 	defer session.Close()

// 	params := map[string]string{
// 		"where.age.gt": "20",
// 		"order":        "age.desc",
// 	}

// 	page := 2
// 	size := 5

// 	qb := orm.NewQueryBuilder(session, orm.Options{})
// 	qb.Bind(params)
// 	require.NoError(t, qb.Error())

// 	session.Limit(size, (page-1)*size)

// 	log.Reset()
// 	var result []TestModel
// 	err := session.Find(&result)
// 	require.NoError(t, err)

// 	require.NotEmpty(t, log.Entries(), "未记录到任何 SQL 查询")
// 	logged := log.Entries()[0]

// 	expectedSQL := "SELECT `id`, `name`, `age`, `is_enabled`, `created_at` FROM `test_model` WHERE `age`>? ORDER BY `age` desc LIMIT ? OFFSET ?"
// 	expectedArgs := []any{"20", 5, 5}

// 	expectedQuery, err := builder.ConvertToBoundSQL(expectedSQL, expectedArgs)
// 	require.NoError(t, err)

// 	loggedQuery, err := builder.ConvertToBoundSQL(logged.SQL, logged.Args)
// 	require.NoError(t, err)

// 	assert.Equal(t, expectedQuery, loggedQuery)
// }

// func TestJoinQuery(t *testing.T) {
// 	db, log := initTestModel(t)
// 	_ = log

// 	session := db.NewSession()
// 	defer session.Close()

// 	// 插入用于关联测试的数据
// 	_, err := session.Insert(&TestAgeModel{Age: 25, AgeDesc: "二十五岁，而立之年"})
// 	require.NoError(t, err)
// 	_, err = session.Insert(&TestModel{Name: "user1", Age: 25})
// 	require.NoError(t, err)
// 	_, err = session.Insert(&TestModel{Name: "user2", Age: 30})
// 	require.NoError(t, err)

// 	// 模拟
// 	params := map[string]string{
// 		"where.b#age_desc.like": "二十五",
// 	}

// 	// 演示如何使用 Join 查询
// 	page, pagesize, _, err := orm.BindQueryStringWithPage(session, params)
// 	require.NoError(t, err)
// 	require.Greater(t, pagesize, 0)
// 	require.GreaterOrEqual(t, page, 0)

// 	type Row struct {
// 		TestModel `xorm:"extends"`
// 		AgeDesc   string `xorm:"'age_desc'" json:"age_desc"`
// 	}
// 	rows := make([]Row, 0)
// 	count, err := session.
// 		Table("test_model").
// 		Join("LEFT", "test_age_model as b", "b.age = test_model.age").
// 		Select("test_model.*, b.age_desc as age_desc").
// 		Limit(pagesize, page*pagesize).
// 		FindAndCount(&rows)
// 	require.NoError(t, err)
// 	require.Equal(t, int64(1), count)
// 	require.Equal(t, 1, len(rows))
// 	require.Equal(t, "二十五岁，而立之年", rows[0].AgeDesc)
// 	// testutil.PrintPretty(rows)

// }

// func TestBindQuerySimple(t *testing.T) {
// 	db, log := initTestModel(t)
// 	session := db.NewSession()
// 	defer session.Close()

// 	params := map[string]string{
// 		// "where.age.gt": "20",
// 		"order":      "age.desc",
// 		"q.age.name": "10",
// 	}

// 	err := orm.BindQueryString(session, params)
// 	require.NoError(t, err)

// 	log.Reset()
// 	var result []TestModel
// 	err = session.Find(&result)
// 	require.NoError(t, err)

// 	sql, args := session.LastSQL()
// 	println(sql, args)
// }
