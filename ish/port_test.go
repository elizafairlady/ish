package ish

import (
	"testing"

	"ish/core"
)

// Ports — exploration.
//
// In both reference models a "port" is the boundary to a resource: in Erlang a
// port behaves like a process you send to and receive from; in Racket a port is
// an input/output stream object. This file feels out what ports look like in
// ish. The finding: a port is naturally a PROCESS (the Erlang model), and the
// entire port abstraction — lifecycle, the synchronous request/reply protocol,
// input vs. output behaviour — is expressible in ish over the existing actor
// primitives (spawn / send / receive / self) with NO new Go.
//
// What would remain irreducible for real OS I/O is only the raw resource access
// at the bottom of a port process: primitives like `fd-open` / `fd-read` /
// `fd-write` / `fd-close` (or socket equivalents) that perform the syscall. The
// port process owns that handle and exposes it through the ish-level protocol
// below; the protocol itself never needs to be in Go.
const portLib = `
# A uniform synchronous call: send {:call msg self}, await {:reply r}.
defn call port msg do
  send port {:call msg self} ;
  receive do {:reply r} -> r end
end

# Output port: a process owning an accumulating buffer resource.
defn buffer-loop contents do
  receive do
    {:call {:write s} from} -> do
      send from {:reply :ok} ;
      buffer-loop (str-concat contents s)
    end
    {:call :contents from} -> do
      send from {:reply contents} ;
      buffer-loop contents
    end
  end
end
defn open-buffer do spawn (fn [[] (buffer-loop "")]) end

# Input port: a process yielding successive items, then :eof forever.
defn reader-loop items do
  receive do
    {:call :read from} -> match items do
      :nil -> do send from {:reply :eof} ; reader-loop :nil end
      (h . t) -> do send from {:reply h} ; reader-loop t end
    end
  end
end
defn open-reader items do spawn (fn [[] (reader-loop items)]) end
`

func evalPort(t *testing.T, src string) core.Datum {
	t.Helper()
	v, err := NewRuntime().EvalSource("port", portLib+src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	d, _ := v.(core.Datum)
	return d
}

// An output port accumulates writes and reports its contents — the whole
// stream object is a process plus an ish-level protocol.
func TestPort_OutputBuffer(t *testing.T) {
	got := evalPort(t,
		"p = open-buffer\n"+
			"call p {:write \"Hello, \"}\n"+
			"call p {:write \"world!\"}\n"+
			"call p :contents")
	if got != core.String("Hello, world!") {
		t.Fatalf("buffer contents = %#v, want \"Hello, world!\"", got)
	}
}

// An input port yields a sequence then signals end-of-input, like reading a
// file to EOF — again purely in ish.
func TestPort_InputReaderToEOF(t *testing.T) {
	got := evalPort(t,
		"r = open-reader '(10 20 30)\n"+
			"a = call r :read\n"+
			"b = call r :read\n"+
			"c = call r :read\n"+
			"d = call r :read\n"+
			"tuple a b c d")
	want := core.Tuple{core.Int(10), core.Int(20), core.Int(30), core.Atom("eof")}
	if !core.DatumEqual(got, want) {
		t.Fatalf("reader sequence = %#v, want %#v", got, want)
	}
}
