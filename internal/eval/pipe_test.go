package eval

import "testing"

// =====================================================================
// Byte pipes
// =====================================================================

func TestPipe_Simple(t *testing.T) {
	run(t, "echo hello | cat", "hello\n")
}

func TestPipe_Chain(t *testing.T) {
	run(t, "echo abc | cat | cat | cat", "abc\n")
}

func TestPipe_WithGrep(t *testing.T) {
	run(t, `echo "apple\nbanana\ncherry" | grep an`, "banana\n")
}

func TestPipe_ExitCodeLast(t *testing.T) {
	run(t, "false | true; echo $?", "0\n")
}

func TestPipe_Pipefail(t *testing.T) {
	run(t, "set -o pipefail; false | true; echo $?", "1\n")
}

// =====================================================================
// Value pipe |>
// =====================================================================

func TestPipeFn_Simple(t *testing.T) {
	run(t, "fn double x do x * 2 end\nfn inc x do x + 1 end\nr = 5 |> double |> inc\necho $r", "11\n")
}

func TestPipeFn_WithLambda(t *testing.T) {
	run(t, `r = 10 |> \x -> x * 2`+"\necho $r", "20\n")
}

func TestPipeFn_WithModuleCall(t *testing.T) {
	run(t, `r = [3, 1, 2] |> List.sort`+"\necho $r", "[1, 2, 3]\n")
}

func TestPipeFn_WithModuleCallArgs(t *testing.T) {
	run(t, `r = [1, 2, 3] |> List.map(\x -> x * 2)`+"\necho $r", "[2, 4, 6]\n")
}

func TestPipeFn_ChainModules(t *testing.T) {
	run(t, `fn double x do x * 2 end
fn inc x do x + 1 end
r = [1, 2, 3] |> List.map(\x -> double(x)) |> List.map(\x -> inc(x))
echo $r`, "[3, 5, 7]\n")
}

// =====================================================================
// Auto-coercion: value | cmd
// =====================================================================

func TestPipe_ListToCmd(t *testing.T) {
	run(t, "[1, 2, 3] | cat", "1\n2\n3\n")
}

func TestPipe_ScalarToCmd(t *testing.T) {
	run(t, "42 | cat", "42\n")
}

func TestPipe_MapToCmd(t *testing.T) {
	run(t, `%{a: 1, b: 2} | sort`, "a: 1\nb: 2\n")
}

// =====================================================================
// Value pipeline (|> is pure value context)
// =====================================================================

func TestPipeFn_CmdToValue(t *testing.T) {
	run(t, `"3\n1\n2" |> String.split("\n") |> List.sort |> String.join("\n") |> \s -> echo $s`, "1\n2\n3\n")
}

// =====================================================================
// Mixed pipes
// =====================================================================

func TestPipe_ValuePipeToCmd(t *testing.T) {
	run(t, `r = [1, 2, 3] |> List.map(\x -> x * 10)
echo $r`, "[10, 20, 30]\n")
}
