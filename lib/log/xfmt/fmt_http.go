package xfmt

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/openfoundry/lib/env"
)

// http.log 格式
type HttpLogEntry struct {
	Time         time.Time `json:"time"`
	Id           string    `json:"reqid"`
	RemoteIp     string    `json:"ip"`
	Host         string    `json:"host"`
	Referer      string    `json:"referer"`
	Method       string    `json:"method"`
	Uri          string    `json:"uri"`
	UserAgent    string    `json:"user_agent"`
	Status       int       `json:"status"`
	Error        string    `json:"error"`
	Latency      int       `json:"latency"`
	LatencyHuman string    `json:"latency_human"`
	BytesIn      string    `json:"bytes_in"`
	BytesOut     int       `json:"bytes_out"`
	RequestBody  string    `json:"request_body"`
}

func (p FmtMainPrinter) printHttpLog(raw []byte) string {
	var e HttpLogEntry
	err := json.Unmarshal(raw, &e)
	if err != nil {
		// 直接输出raw
		fmt.Printf("printHttpLog error: %v\n", err)
		return string(raw)
	}

	list := make([]string, 0, 16)

	I := strconv.Itoa
	B := func(s string) string {
		if e.Latency > 100_000_000 {
			return color.RedString(s)
		}
		// return color.BlackString(s)
		return s
	}
	R := color.RedString
	C := color.CyanString
	URI := func() string {
		return fmt.Sprintf("%-4s %-24s", e.Method, e.Uri)
	}
	BYTES := func() string {
		bytesIn := 0
		if e.BytesIn != "" {
			bytesIn, _ = strconv.Atoi(e.BytesIn)
		}
		bytesOut := 0
		bytesOut = e.BytesOut
		return fmt.Sprintf("%4s %6s", I(bytesIn), I(bytesOut))
	}()
	LATENCY := func() string {
		// ms := float32(e.Latency) / 1000_000
		// s := fmt.Sprintf("%7.2f", ms)
		// return B(s)
		if e.Latency > 200 {
			return color.RedString(e.LatencyHuman)
		}
		return e.LatencyHuman

	}()
	// IP := func() string {
	// 	return fmt.Sprintf("%-14s", e.RemoteIp)
	// }()
	TIME := func() string {
		return B(e.Time.Local().Format("01-02 15:04:05.000"))
	}()
	STATUS := func() string {
		if e.Status != 200 {
			return color.RedString(I(e.Status))
		}
		return I(e.Status)
	}()
	REQID := func() string {
		return color.MagentaString(e.Id)
	}()

	// 格式化输出
	list = append(list, TIME)
	// list = append(list, IP)
	list = append(list, STATUS)
	list = append(list, LATENCY)
	list = append(list, REQID)
	list = append(list, BYTES)
	list = append(list, C(URI()))
	list = append(list, R(e.Error))

	var builder strings.Builder
	builder.WriteString(strings.Join(list, " "))
	if env.IsDebug() {
		if len(e.RequestBody) > 512 {
			builder.WriteString(" " + truncateString(e.RequestBody, 128))
		} else {
			builder.WriteString(" " + e.RequestBody)
		}
	}
	builder.WriteString("\n")
	return builder.String()
}

func truncateString(data string, limit int) string {
	if limit <= 0 {
		return ""
	}
	text := data
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
