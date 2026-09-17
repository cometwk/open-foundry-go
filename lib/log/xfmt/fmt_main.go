package xfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/fatih/color"
)

type MainLogEntry struct {
	Time    time.Time `json:"time"`
	Error   string    `json:"error"`
	File    string    `json:"file"`
	Func    string    `json:"func"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Method  string    `json:"method"`
	Url     string    `json:"url"`
}

type FmtMainPrinter struct {
	Out io.Writer
}

func NewFmtMainPrinter(w io.Writer) io.Writer {
	return &FmtMainPrinter{
		Out: w,
	}
}

func (p FmtMainPrinter) Write(raw []byte) (n int, err error) {
	var e map[string]any
	err = json.Unmarshal(raw, &e)
	o := p.Out
	if err != nil {
		// 直接输出raw
		return o.Write(raw)
	}
	s := p.printEntry(e)
	return o.Write([]byte(s))
}

func (p FmtMainPrinter) printEntry(data map[string]interface{}) string {
	if data["module"] == "httplog" {
		bytes, err := json.Marshal(data)
		if err != nil {
			return ""
		}
		return p.printHttpLog(bytes)
	}
	return p.printMainLog(data)
}
func (p FmtMainPrinter) printMainLog(data map[string]interface{}) string {
	list := make([]string, 0, 16)
	errorMsg, isError := data["error"]

	M := color.MagentaString
	// I := strconv.Itoa
	R := color.RedString
	// B := color.BlackString
	B := func(s string) string {
		if isError {
			return color.RedString(s)
		}
		//return color.BlackString(s)
		return s
	}
	DIM := color.CyanString

	FILE := func() string {
		if v, ok := data["file"]; ok {
			return B(fmt.Sprintf("%-20s", v))
		}
		return ""
	}()
	MODULE := func() string {
		if v, ok := data["module"]; ok {
			return M(fmt.Sprintf("%-20s", v))
		}
		return ""
	}()
	REQID := func() string {
		if v, ok := data["reqid"]; ok && v != nil {
			return M(fmt.Sprintf("%s", v))
		}
		return ""
	}()
	ID := func() string {
		if v, ok := data["id"]; ok && v != nil {
			return R(fmt.Sprintf("%s", v))
		}
		return ""
	}()

	//FUNC := func() string {
	//	return B(fmt.Sprintf("%-20s", e.Func))
	//}()
	LEVEL := func() string {
		if v, ok := data["level"]; ok {
			levelStr := strings.ToLower(v.(string))
			var level slog.Level
			if err := level.UnmarshalText([]byte(levelStr)); err == nil && level >= slog.LevelWarn {
				return R(fmt.Sprintf("%-7s", levelStr))
			}
			return fmt.Sprintf("%-7s", levelStr)
		}
		return ""
	}()
	MESSAGE := func() string {
		if v, ok := data["message"]; ok {
			return B(fmt.Sprintf("%-20s", v))
		}
		return ""
	}()
	TIME := func() string {
		if v, ok := data["time"]; ok {
			t, err := time.Parse(time.RFC3339, v.(string))
			if err != nil {
				return ""
			}
			return B(t.Local().Format("01-02 15:04:05.000"))
		}
		return ""
	}()
	WSID := func() string {
		// websocket id
		if v, ok := data["wsid"]; ok {
			//return B(fmt.Sprintf("%-20s", v))
			return v.(string)
		}
		return ""
	}()

	// 格式化输出
	list = append(list, LEVEL)
	list = append(list, TIME)
	_, ok := data["module"]
	if ok {
		list = append(list, MODULE)
	} else {
		list = append(list, FILE)
	}
	list = append(list, WSID)
	//list = append(list, FUNC)
	list = append(list, ":")
	list = append(list, REQID)
	list = append(list, ID)
	list = append(list, MESSAGE)

	processedKeys := map[string]struct{}{
		"error":   {},
		"file":    {},
		"module":  {},
		"reqid":   {},
		"id":      {},
		"level":   {},
		"message": {},
		"time":    {},
		"wsid":    {},
	}
	extra := make(map[string]any, len(data))
	for k, v := range data {
		if _, ok := processedKeys[k]; !ok {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		if bytes, err := json.Marshal(extra); err == nil {
			list = append(list, DIM(string(bytes)))
		}
	}

	// 构建最终输出字符串
	var builder strings.Builder
	builder.WriteString(strings.Join(list, " "))
	builder.WriteString("\n")
	if isError && errorMsg != nil {
		builder.WriteString("\t")
		builder.WriteString(R(errorMsg.(string)))
		builder.WriteString("\n")
	}
	return builder.String()
}
