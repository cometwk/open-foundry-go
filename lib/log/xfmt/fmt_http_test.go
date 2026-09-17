package xfmt

import (
	"testing"
)

func Test_Http_Print(t *testing.T) {
	// var s1 = `{"time":"2023-07-21T14:58:51.05826+08:00","id":"PanDPiD3EhSDG7yoOW4BAnuj16dQ5EQk","remote_ip":"127.0.0.1","host":"localhost:4444","referer":"http://localhost:3333/signin","method":"GET","uri":"/login/signin/settings","user_agent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36","status":404,"error":"code=404, message=Not Found","latency":50450701,"latency_human":"50.450701ms","bytes_in":0,"bytes_out":71}`
	// var s2 = `{"time":"2023-07-22T17:25:09.075282+08:00","id":"AMfgeITXmWNFXN36s8tyd5FyBMlIqmuA","remote_ip":"127.0.0.1","host":"localhost:4444","referer":"http://localhost:5173/p2018/sheet","method":"POST","uri":"/graphql","user_agent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36","status":200,"error":"","latency":3669923,"latency_human":"3.669923ms","bytes_in":419,"bytes_out":2766}`
	// color.NoColor = false

	// printer := NewFmtHttpPrinter(os.Stdout)
	// printer.Print([]byte(s1))
	// printer.Print([]byte(s2))
}
