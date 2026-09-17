package orm_test

// import (
// 	"context"
// 	"fmt"
// 	"log/slog"
// 	"testing"
// 	"time"

// 	"github.com/openfoundry/liborm"
// 	"github.com/openfoundry/libtestdbutil"
// 	"github.com/stretchr/testify/assert"
// )

// func TestBizDate(t *testing.T) {

// 	t.Run("t0", func(t *testing.T) {
// 		var makeTime time.Time
// 		makeTime = time.Date(2026, 6, 5, 1, 0, 0, 0, time.Local)
// 		fmt.Println(makeTime.Format(time.DateOnly))

// 		makeTime = time.Date(2026, 6, 5, 1, 0, 0, 0, time.Local).UTC()
// 		fmt.Println(makeTime.Format(time.DateOnly))

// 		fmt.Println("----")

// 		makeTime = time.Date(2026, 6, 5, 20, 0, 0, 0, time.UTC)
// 		fmt.Println(makeTime.Format(time.DateOnly))

// 		fmt.Println(makeTime.Local().Format(time.DateOnly))
// 	})

// }

// func TestExample(t *testing.T) {
// 	// 连接数据库
// 	opts := testdbutil.New().
// 		// DDLDir("/pkg/nanoq/docs/ddl").
// 		// TableNames(
// 		// 	"nanoq_tasks",
// 		// 	"nanoq_task_history",
// 		// 	"nanoq_idempotent",
// 		// 	"nanoq_replay",
// 		// ).
// 		LogLevel(slog.LevelDebug).
// 		DDL(true)

// 	testdbutil.SetupDB(nil, opts)

// 	// 获取会话
// 	session := orm.MustSession(context.Background())
// 	defer session.Close()

// 	t.Run("t0", func(t *testing.T) {
// 		date1, err := orm.NewBizDate("2026-05-20")
// 		assert.NoError(t, err)
// 		date2, err := orm.NewBizDate("20260520")
// 		assert.NoError(t, err)
// 		assert.Equal(t, date1, date2)

// 		assert.Equal(t, "20260520", date1.Format("20060102"))
// 		assert.Equal(t, "20260520", date2.Format("20060102"))
// 	})

// 	t.Run("t1", func(t *testing.T) {
// 		createdAt := time.Now().Truncate(time.Second)
// 		id := snowflake.SnowflakeId()
// 		arrival := biz.BizDate(time.Now().AddDate(0, 0, 100).Format("2006-01-02"))

// 		// mock data
// 		entity := biz.MOrder{
// 			ID:         id,
// 			CustomerNo: "CUST123456",
// 			RuleNo:     "RULE001",
// 			OutOrdno:   "ORD987654",
// 			ClientIP:   "127.0.0.1",
// 			Attach:     "mock_attach",
// 			Body:       "mock_body",
// 			NotifyURL:  "https://example.com/notify",
// 			PymdCd:     "01",
// 			PyOrdrTpcd: "04",
// 			TxnTamt:    123.45,
// 			PayStatus:  2,
// 			CreatedAt:  createdAt,
// 			ArrivalDt:  arrival,
// 		}
// 		_, err := session.Insert(&entity)
// 		assert.NoError(t, err)

// 		assert.Equal(t, id, entity.ID)
// 		assert.Equal(t, "CUST123456", entity.CustomerNo)
// 		assert.Equal(t, 123.45, entity.TxnTamt)
// 	})

// 	t.Run("t2", func(t *testing.T) {
// 		var entity biz.MOrder
// 		ok, err := session.Where("id = ?", 2062764692555698176).Get(&entity)
// 		assert.NoError(t, err)
// 		assert.True(t, ok)
// 		fmt.Println(entity.ArrivalDt)

// 	})

// }
