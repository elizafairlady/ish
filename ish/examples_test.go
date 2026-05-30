package ish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ish/core"
)

func readExample(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "examples", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(src)
}

// The log-analytics pipeline example runs end to end and returns its headline
// metrics {total errors bytes}.
func TestExampleLogReport(t *testing.T) {
	v, err := NewRuntime().EvalSource("log_report", readExample(t, "log_report.ish"))
	if err != nil {
		t.Fatalf("log_report.ish: %v", err)
	}
	want := core.Tuple{core.Int(7), core.Int(2), core.Int(3456)}
	if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
		t.Fatalf("log_report metrics = %#v, want %#v", d, want)
	}
}

// The HTTP server example: load its definitions (everything before the blocking
// entry point), start it on an ephemeral port, and make a real request through
// the routing/parsing/formatting code from the file.
func TestExampleHTTPServer(t *testing.T) {
	src := readExample(t, "http_server.ish")
	defs, _, ok := strings.Cut(src, "listener = tcp-listen 8080")
	if !ok {
		t.Fatal("http_server.ish: expected the `listener = tcp-listen 8080` entry line")
	}
	harness := "\nl = tcp-listen 0\n" +
		"p = tcp-listener-port l\n" +
		"spawn (fn do serve l end)\n" +
		"c = tcp-connect \"127.0.0.1\" p\n" +
		"tcp-send c \"GET /about HTTP/1.1\\r\\nHost: x\\r\\n\\r\\n\"\n" +
		"resp = tcp-recv c\n" +
		"close c\n" +
		"resp"
	v, err := NewRuntime().EvalSource("http_server", defs+harness)
	if err != nil {
		t.Fatalf("http_server.ish: %v", err)
	}
	resp, ok := v.(core.String)
	if !ok {
		t.Fatalf("response = %#v, want string", v)
	}
	if !strings.Contains(string(resp), "200 OK") || !strings.Contains(string(resp), "about us") {
		t.Fatalf("response did not route /about:\n%s", resp)
	}

	// A second request to an unknown path must 404 through the same server.
	miss := defs + "\nl = tcp-listen 0\np = tcp-listener-port l\n" +
		"spawn (fn do serve l end)\n" +
		"c = tcp-connect \"127.0.0.1\" p\n" +
		"tcp-send c \"GET /nope HTTP/1.1\\r\\n\\r\\n\"\n" +
		"r = tcp-recv c\nclose c\nr"
	v2, err := NewRuntime().EvalSource("http_server", miss)
	if err != nil {
		t.Fatalf("http_server.ish (404): %v", err)
	}
	if r, _ := v2.(core.String); !strings.Contains(string(r), "404 Not Found") {
		t.Fatalf("unknown path did not 404:\n%s", r)
	}
}

// The file primitives round-trip.
func TestFileIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.txt")
	src := "file-write \"" + path + "\" \"line one\\nline two\"\n" +
		"file-read \"" + path + "\""
	v, err := NewRuntime().EvalSource("file", src)
	if err != nil {
		t.Fatalf("file io: %v", err)
	}
	if v != core.String("line one\nline two") {
		t.Fatalf("file-read = %#v", v)
	}
}

// The UDP echo server example: load its definitions, run it on an ephemeral
// port, and round-trip a datagram through it.
func TestExampleUDPEcho(t *testing.T) {
	src := readExample(t, "udp_echo.ish")
	defs, _, ok := strings.Cut(src, "server = udp-open 9000")
	if !ok {
		t.Fatal("udp_echo.ish: expected the `server = udp-open 9000` entry line")
	}
	harness := "\ns = udp-open 0\n" +
		"p = udp-port s\n" +
		"spawn (fn do echo-loop s end)\n" +
		"c = udp-open 0\n" +
		"udp-send c (str-concat \"127.0.0.1:\" (to-string p)) \"ping\"\n" +
		"match (udp-recv c) do {data from} -> data end"
	v, err := NewRuntime().EvalSource("udp_echo", defs+harness)
	if err != nil {
		t.Fatalf("udp_echo.ish: %v", err)
	}
	if v != core.String("echo: ping") {
		t.Fatalf("udp echo = %#v, want \"echo: ping\"", v)
	}
}

// A request larger than the tcp-recv buffer (4096B) forces read-request to loop
// across multiple reads, exercising the in-language TCP framing rather than
// relying on a single read holding the whole request.
func TestExampleHTTPServerLargeRequest(t *testing.T) {
	src := readExample(t, "http_server.ish")
	defs, _, _ := strings.Cut(src, "listener = tcp-listen 8080")
	big := "GET /about HTTP/1.1\\r\\nX-Pad: " + strings.Repeat("a", 5000) + "\\r\\n\\r\\n"
	harness := "\nl = tcp-listen 0\np = tcp-listener-port l\n" +
		"spawn (fn do serve l end)\n" +
		"c = tcp-connect \"127.0.0.1\" p\n" +
		"tcp-send c \"" + big + "\"\n" +
		"r = tcp-recv c\nclose c\nr"
	v, err := NewRuntime().EvalSource("http_server", defs+harness)
	if err != nil {
		t.Fatalf("http_server.ish (large): %v", err)
	}
	if r, _ := v.(core.String); !strings.Contains(string(r), "200 OK") || !strings.Contains(string(r), "about us") {
		t.Fatalf("large request did not route /about (response head): %.120s", string(r))
	}
}

// The config example: a file-write/file-read round-trip whose contents are
// parsed into a dict and looked up.
func TestExampleConfig(t *testing.T) {
	v, err := NewRuntime().EvalSource("config", readExample(t, "config.ish"))
	if err != nil {
		t.Fatalf("config.ish: %v", err)
	}
	want := core.Tuple{core.String("localhost"), core.String("8080")}
	if d, _ := v.(core.Datum); !core.DatumEqual(d, want) {
		t.Fatalf("config = %#v, want %#v", d, want)
	}
}
