package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/labstack/echo/v5"
)

// Only for dev mode
func init() {
	color.NoColor = false
}

func dumpRequest(c *echo.Context) {
	req := c.Request()
	url := req.URL.String()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		slog.Error("Unable to read request body", "error", err)
		return
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	reqC := color.New(color.FgHiGreen, color.Bold)

	fmt.Println()
	reqC.Printf("请求首部: %s %s\n", req.Method, url)
	reqC.Println("===============================================")
	dumpHeader(req.Header)

	fmt.Println()
	n := fmt.Sprintf("%d", len(string(body)))
	reqC.Printf("请求数据: %s %s %s bytes\n", req.Method, url, n)
	reqC.Println("===============================================")

	// Request mime type
	mimetype := req.Header.Get("content-type")

	// Don't dump multipart/form-data response body
	if strings.HasPrefix(mimetype, echo.MIMEMultipartForm) {
		color.HiRed("Skip beacuse content-type is %s\n", color.HiGreenString(mimetype))
	} else {
		if len(body) > 512 {
			color.HiMagenta(truncateUTF8(body, 128))
		} else {
			color.HiMagenta(string(body))
		}
	}
}

type dumpWriter struct {
	http.ResponseWriter

	body   *bytes.Buffer
	status int
	size   int64
}

func newDumpWriter(w http.ResponseWriter) *dumpWriter {
	return &dumpWriter{
		ResponseWriter: w,
		body:           new(bytes.Buffer),
	}
}

func (w *dumpWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *dumpWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	n, err := w.ResponseWriter.Write(p)

	if n > 0 {
		_, _ = w.body.Write(p[:n])
		w.size += int64(n)
	}

	return n, err
}

var _ http.ResponseWriter = &dumpWriter{}

func (dw *dumpWriter) dumpResponse(req *http.Request) {
	res, err := echo.UnwrapResponse(dw.ResponseWriter)
	if err != nil {
		slog.Error("Unable to unwrap response", "error", err)
		return
	}

	url := req.URL.String()

	color.NoColor = false

	resC := color.New(color.FgHiMagenta, color.Bold)

	fmt.Println()
	resC.Printf("响应首部: %s %s %d\n", req.Method, url, res.Status)
	resC.Println("===============================================")
	dumpHeader(res.Header())

	fmt.Println()
	n := fmt.Sprintf("%d", res.Size)
	resC.Printf("响应数据: %s %s %s bytes\n", req.Method, url, n)
	resC.Println("===============================================")

	// Response mime type
	mimetype := res.Header().Get("content-type")

	if strings.HasPrefix(mimetype, "image/") {
		color.HiRed("Skip beacuse content-type is %s\n", color.HiGreenString(mimetype))
		return
	}
	if strings.HasPrefix(mimetype, "font/") {
		color.HiRed("Skip beacuse content-type is %s\n", color.HiGreenString(mimetype))
		return
	}
	if strings.HasPrefix(mimetype, "text/javascript") {
		color.HiRed("Skip beacuse content-type is %s\n", color.HiGreenString(mimetype))
		return
	}
	if strings.HasPrefix(mimetype, "application/vnd") {
		color.HiRed("Skip beacuse content-type is %s\n", color.HiGreenString(mimetype))
		return
	}
	if strings.HasSuffix(url, ".js.map") {
		color.HiRed("Skip *.js.map beacuse it's content is too long\n")
		return
	}

	// display preview xfmt if possible
	if strings.HasPrefix(mimetype, echo.MIMEApplicationJSON) {
		var r map[string]string

		if err := json.Unmarshal(dw.body.Bytes(), &r); err == nil {
			color.HiRed("{")
			for k, v := range r {
				if len(v) <= 1001 {
					color.HiYellow("  %s: %s,", k, color.HiCyanString(v))
				} else {
					l := color.HiGreenString("%s bytes", fmt.Sprintf("%d", len(v)))
					color.HiYellow("  %s: %s...\n%s,", k, color.HiCyanString(v[:1000]), l)
				}
			}
			color.HiRed("}")
			return
		}
	}

	text := dw.body.String()
	if len(text) <= 1001 {
		color.HiCyan("%s", text)
	} else {
		l := color.HiGreenString("%s bytes", fmt.Sprintf("%d", len(text)))
		color.HiCyan("%s...\n%s", text[:1000], l)
	}
}

// Dump header
// The output will be sort by header key
func dumpHeader(header http.Header) {
	keys, i := make([]string, len(header)), 0

	for k := range header {
		keys[i] = k
		i += 1
	}
	sort.Strings(keys)

	keyC := color.New(color.FgHiYellow)
	valC := color.New(color.FgHiCyan)

	for _, k := range keys {
		keyC.Print(k)
		fmt.Print(": ")
		valC.Println(strings.Join(header[k], ","))
	}
}

func truncateUTF8(data []byte, limit int) string {
	if limit <= 0 {
		return ""
	}
	text := string(data)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func dumpMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		dumpRequest(c) // 立即打印

		dw := newDumpWriter(c.Response())
		c.SetResponse(dw)

		defer dw.dumpResponse(c.Request()) // 最后打印

		return next(c)
	}
}
