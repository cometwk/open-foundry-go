package orm

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BizDate 表示业务日期（MySQL DATE），格式为 YYYY-MM-DD。
// DATETIME 采用 UTC，DATE 采用本地时间，故 DATE 映射为 BizDate（string）。
type BizDate string

func (d BizDate) String() string { return string(d) }

func (d BizDate) Format(layout string) string {
	t, err := time.Parse("2006-01-02", string(d))
	if err != nil {
		return string(d)
	}
	return t.Format(layout)
}

func (d BizDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(d))
}

func (d *BizDate) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := NewBizDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// NewBizDate 从字符串创建 BizDate, 支持 YYYYMMDD 和 YYYY-MM-DD 格式
func NewBizDate(s string) (BizDate, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty date")
	}
	if len(s) == 8 && isDigits(s) {
		return BizDate(s[:4] + "-" + s[4:6] + "-" + s[6:8]), nil
	}
	if len(s) >= 10 && s[4] == '-' {
		return BizDate(s[:10]), nil
	}
	return "", fmt.Errorf("invalid date %q", s)
}

func NewDateOnly(t time.Time) BizDate {
	return BizDate(t.Format(time.DateOnly))
}
func NewToday() BizDate {
	return NewDateOnly(time.Now())
}

func (d BizDate) Value() (driver.Value, error) {
	// BizDate 必须 NOT NULL，避免麻烦
	return string(d), nil
}

func (d *BizDate) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		// Scan 2026-09-13 00:00:00 +0000 UTC
		// => 2026-09-13
		*d = BizDate(v.Format("2006-01-02"))
		return nil
	case []byte:
		s := string(v)
		if len(s) >= 10 {
			*d = BizDate(s[:10])
			return nil
		}
		return fmt.Errorf("BizDate: invalid DATE bytes %q", s)
	case string:
		if len(v) >= 10 {
			*d = BizDate(v[:10])
			return nil
		}
		return fmt.Errorf("BizDate: invalid DATE string %q", v)
	case nil:
		*d = ""
		return nil
	default:
		return fmt.Errorf("BizDate: unsupported scan type %T", src)
	}
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var _ sql.Scanner = (*BizDate)(nil)
var _ driver.Valuer = (*BizDate)(nil)
