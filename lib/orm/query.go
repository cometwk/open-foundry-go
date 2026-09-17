package orm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openfoundry/lib/orm/mybuilder"
	"github.com/pkg/errors"
	"xorm.io/builder"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

type Options struct {
	TableName       string   // 表名, 用于处理联表查询, 强制添加 table#field 到sql中
	SelectWhitelist []string // Select 列名白名单
	WhereWhitelist  []string // Where 列名白名单 (也用于 Order)
	QWhitelist      []string // Q 列名白名单
}

type queryBuilder struct {
	Session *xorm.Session
	Options Options           // 选项
	Extra   map[string]string // 未被解析的查询参数
	errors  []error
}

func NewQueryBuilder(session *xorm.Session, options Options) *queryBuilder {
	return &queryBuilder{
		Session: session,
		Options: options,
		Extra:   make(map[string]string),
		errors:  make([]error, 0),
	}
}

// validateColumn 检查列名是否在指定的白名单中
func (qb *queryBuilder) validateColumn(column string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, s := range whitelist {
		if s == column {
			return true
		}
	}
	return false
}

// keyError 将查询参数解析错误登记到错误列表中
func (qb *queryBuilder) keyError(key, reason string) {
	err := fmt.Errorf("参数 '%s' 无效: %s", key, reason)
	qb.errors = append(qb.errors, err)
}

// Error 合并所有解析错误并返回一个单一的 error 对象
func (qb *queryBuilder) Error() error {
	if len(qb.errors) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("查询参数解析错误: ")
	for i, err := range qb.errors {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(err.Error())
	}
	return errors.New(sb.String())
}

// parseColumn 解析列名，并为各部分添加反引号. 支持 table#column 的格式
func parseColumn(column string, table string) string {
	// e.g. user_profile#city -> `user_profile`.`city`
	//      age -> `age`
	column = strings.ReplaceAll(column, "#", ".")
	parts := strings.Split(column, ".")
	if table != "" && len(parts) == 1 {
		// 强制添加表名到列名前
		parts = append([]string{table}, parts...)
	}
	quotedParts := make([]string, len(parts))
	for i, p := range parts {
		quotedParts[i] = fmt.Sprintf("`%s`", p)
	}
	return strings.Join(quotedParts, ".")
}

func validateDate(date string) error {
	_, err := time.Parse("2006-01-02", date)
	if err != nil {
		return fmt.Errorf("date 格式必须为 YYYY-MM-DD")
	}
	return nil
}

func (qb *queryBuilder) Where(key string, value string) *queryBuilder {
	// 解析查询键，格式: where.column.op=value
	// 关联表查询格式: where.table#column.op=value
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		qb.keyError(key, "格式错误, 应为 'where.列名.操作符'")
		return qb
	}

	column := parts[1]
	op := parts[2]

	if len(column) == 0 {
		qb.keyError(key, "列名不能为空")
		return qb
	}

	if !qb.validateColumn(column, qb.Options.WhereWhitelist) {
		qb.keyError(key, fmt.Sprintf("列名 '%s' 不在白名单中", column))
		return qb
	}

	column = parseColumn(column, qb.Options.TableName)
	session := qb.Session

	switch op {
	case "eq":
		session.Where(builder.Eq{column: value})
	case "neq":
		session.Where(builder.Neq{column: value})
	case "gt":
		session.Where(builder.Gt{column: value})
	case "lt":
		session.Where(builder.Lt{column: value})
	case "gte":
		session.Where(builder.Gte{column: value})
	case "lte":
		session.Where(builder.Lte{column: value})
	case "in":
		values := strings.Split(value, ",")
		session.In(column, values)
	case "notIn":
		values := strings.Split(value, ",")
		qb.Session = qb.Session.NotIn(column, values)
	case "like":
		session.Where(builderLike(session, column, value))
	case "likes":
		values := strings.Split(value, ",")
		for _, v := range values {
			session.Where(builderLike(session, column, v))
		}
	case "btw":
		values := strings.Split(value, ",")
		if len(values) != 2 {
			qb.keyError(key, "需要两个值, 用逗号分隔")
			return qb
		}
		session.Where(builder.Between{Col: column, LessVal: values[0], MoreVal: values[1]})
	case "time":
		values := strings.Split(value, ",")
		if len(values) != 2 {
			qb.keyError(key, "需要两个时间值, 用逗号分隔")
			return qb
		}
		startTime, err := time.ParseInLocation("2006-01-02 15:04:05", values[0], time.Local)
		if err != nil {
			qb.keyError(key, fmt.Sprintf("起始时间格式错误: %v", err))
			return qb
		}
		endTime, err := time.ParseInLocation("2006-01-02 15:04:05", values[1], time.Local)
		if err != nil {
			qb.keyError(key, fmt.Sprintf("结束时间格式错误: %v", err))
			return qb
		}
		// session.Where(builder.Between{Col: column, LessVal: startTime.UTC(), MoreVal: endTime.UTC()})
		// 不采用between，要求采用: 左闭右开（最佳实践）
		session.Where(column+" >= ?", startTime.UTC()).And(column+" < ?", endTime.UTC())
	case "date": // 本地时间查询
		values := strings.Split(value, ",")
		if len(values) != 2 {
			qb.keyError(key, "需要两个时间值, 用逗号分隔")
			return qb
		}
		startDate := values[0]
		endDate := values[1]
		if err := validateDate(startDate); err != nil {
			qb.keyError(key, fmt.Sprintf("起始时间格式错误: %v", err))
			return qb
		}
		if err := validateDate(endDate); err != nil {
			qb.keyError(key, fmt.Sprintf("结束时间格式错误: %v", err))
			return qb
		}
		// 日期，采用左闭右闭区间
		session.Where(column+" >= ?", startDate).And(column+" <= ?", endDate)
	case "null":
		if value == "true" {
			session.Where(builder.IsNull{column})
		} else {
			session.Where(builder.NotNull{column})
		}
	default:
		qb.keyError(key, fmt.Sprintf("不支持的操作符 '%s'", op))
	}
	return qb
}

func (qb *queryBuilder) Order(key string, value string) *queryBuilder {
	// 排序格式: order=column1.desc,column2.asc
	session := qb.Session
	options := strings.Split(value, ",")
	for _, x := range options {
		params := strings.Split(x, ".")
		var validParams []string
		for _, p := range params {
			if p != "" {
				validParams = append(validParams, p)
			}
		}
		if len(validParams) == 0 {
			qb.keyError(key, "排序条件不能为空")
			continue
		}
		params = validParams
		if !qb.validateColumn(params[0], qb.Options.WhereWhitelist) {
			qb.keyError(key, fmt.Sprintf("列名 '%s' 不在白名单中", params[0]))
			continue
		}
		column := parseColumn(params[0], qb.Options.TableName)
		order := "asc" // 默认升序
		if len(params) > 1 {
			direction := strings.ToLower(params[1])
			if direction != "asc" && direction != "desc" {
				qb.keyError(key, fmt.Sprintf("排序方向 '%s' 错误, 应为 'asc' 或 'desc'", params[1]))
				continue
			}
			order = direction
		}
		session.OrderBy(fmt.Sprintf("%s %s", column, order))
	}
	return qb
}

func (qb *queryBuilder) Bind(params map[string]string) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := params[key]
		if value == "" {
			continue
		}
		if key == "q" {
			continue
		}
		if strings.HasPrefix(key, "q.") {
			parts := strings.Split(key, ".")
			if len(parts) < 2 {
				qb.keyError(key, "格式错误, 应为 'q.列名1.列名2'")
				continue
			}
			columns := parts[1:]
			cond := builder.Or()
			for _, column := range columns {
				if !qb.validateColumn(column, qb.Options.QWhitelist) {
					qb.keyError(key, fmt.Sprintf("列名 '%s' 不在白名单中", column))
					continue
				}
				column = parseColumn(column, qb.Options.TableName)
				cond = cond.Or(builderLike(qb.Session, column, value))
			}
			qb.Session.Where(cond)
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "where":
			qb.Where(key, value)
		case "select":
			columns := strings.Split(value, ",")
			var validCols []string
			for _, col := range columns {
				if !qb.validateColumn(col, qb.Options.SelectWhitelist) {
					qb.keyError(key, fmt.Sprintf("列名 '%s' 不在白名单中", col))
					continue
				}
				validCols = append(validCols, col)
			}
			if len(validCols) > 0 {
				qb.Session.Cols(validCols...)
			}
		case "order":
			qb.Order(key, value)
		default:
			qb.Extra[key] = value
		}
	}
}

func BindQueryString(session *xorm.Session, params map[string]string) error {
	return BindQueryStringWithOptions(session, params, Options{})
}

func BindQueryStringWithPage(session *xorm.Session, params map[string]string) (page int, pagesize int, extra map[string]string, err error) {
	page, pagesize = 0, 10 // page start with 0
	if p, ok := params["page"]; ok {
		page, err = strconv.Atoi(p)
		if err != nil {
			err = errors.New("invalid page parameter")
			return
		}
		delete(params, "page")
	}
	if ps, ok := params["pagesize"]; ok {
		pagesize, err = strconv.Atoi(ps)
		if err != nil {
			err = errors.New("invalid pagesize parameter")
			return
		}
		delete(params, "pagesize")
	}
	if pagesize > 500 {
		err = errors.New("pagesize 不能超过 500")
		return
	}

	qb := NewQueryBuilder(session, Options{})
	qb.Bind(params)

	if err = qb.Error(); err != nil {
		return
	}

	extra = qb.Extra
	return
}

func builderLike(session *xorm.Session, column string, value string) builder.Cond {
	isPostgreSQL := session.Engine().Dialect().URI().DBType == schemas.POSTGRES
	if isPostgreSQL {
		return mybuilder.ILike{column, value}
	}
	return builder.Like{column, value}
}

// 为了处理联表查询, 强制在查询参数中添加 where.tablename#column.op=value 格式
func BindQueryStringWithTable(session *xorm.Session, params map[string]string, tablename string) error {
	return BindQueryStringWithOptions(session, params, Options{TableName: tablename})
}

func BindQueryStringWithOptions(session *xorm.Session, params map[string]string, options Options) error {
	qb := NewQueryBuilder(session, options)
	qb.Bind(params)

	if len(qb.Extra) > 0 {
		keys := make([]string, 0, len(qb.Extra))
		for k := range qb.Extra {
			keys = append(keys, k)
		}
		session.Engine().Logger().Warnf("发现未被解析的查询参数: %v", keys)
	}

	return qb.Error()
}
