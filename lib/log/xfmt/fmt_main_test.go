package xfmt

import (
	"os"
	"testing"

	"github.com/fatih/color"
)

func Test_Main_Print(t *testing.T) {

	s2 := []string{`
		{
		  "error": "code=404, message=Not Found",
		  "file": "main.go:456",
		  "func": "main.httpErrorHandler",
		  "level": "error",
		  "message": "HTTP服务错误: code=404, message=Not Found，url: /login/signin/settings",
		  "method": "GET",
		  "time": "2023-07-21T14:58:51+08:00",
		  "url": "/login/signin/settings"
		}`,
		`
		{
		  "file": "pagination.go:117",
		  "func": "db.(*Pagination).Exec",
		  "level": "debug",
		  "message": "SQL: SELECT \"ca_insts\".* FROM \"ca_insts\" ORDER BY \"ca_insts\".\"update_at\" DESC, \"ca_insts\".\"update_at\" DESC LIMIT 10",
		  "time": "2023-07-21T14:59:57+08:00",
		  "x-module": "orm", 
		  "reqid": "K2Sn7BHvKeXbSOQD0Ox8wsOn885Ur9Se",
		  "id": "123"
		}
	`,
		`
{
  "app": "demo",
  "file": "helper.go:31",
  "func": "node.performOutgoingBehavior",
  "id": "1",
  "level": "debug",
  "message": "Leaving activity 'start'",
  "reqid": "OqfLBrGqKhdyitJUKUAhSddmrfsuflio",
  "time": "2025-07-01T15:12:34+08:00"
}`,
	}
	color.NoColor = false

	printer := NewFmtMainPrinter(os.Stdout)
	for _, s := range s2 {
		printer.Write([]byte(s))
	}
}
