package builtin

import (
	"fmt"
	"strings"

	"ish/internal/core"
)

func builtinEcho(args []string, env *core.Env) (int, error) {
	newline := true
	escapes := false
	start := 0
	for start < len(args) {
		arg := args[start]
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		valid := true
		for _, ch := range arg[1:] {
			if ch != 'n' && ch != 'e' && ch != 'E' {
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		for _, ch := range arg[1:] {
			switch ch {
			case 'n':
				newline = false
			case 'e':
				escapes = true
			case 'E':
				escapes = false
			}
		}
		start++
	}
	out := strings.Join(args[start:], " ")
	if escapes {
		out = expandEchoEscapes(out)
	}
	w := env.Stdout()
	if newline {
		fmt.Fprintln(w, out)
	} else {
		fmt.Fprint(w, out)
	}
	return 0, nil
}

func expandEchoEscapes(s string) string {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\\' || i+1 >= len(s) {
			buf.WriteByte(s[i])
			i++
			continue
		}
		i++
		switch s[i] {
		case 'n':
			buf.WriteByte('\n')
		case 't':
			buf.WriteByte('\t')
		case 'r':
			buf.WriteByte('\r')
		case 'a':
			buf.WriteByte('\a')
		case 'b':
			buf.WriteByte('\b')
		case 'f':
			buf.WriteByte('\f')
		case 'v':
			buf.WriteByte('\v')
		case '\\':
			buf.WriteByte('\\')
		case '0':
			val := byte(0)
			for j := 0; j < 3 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; j++ {
				i++
				val = val*8 + (s[i] - '0')
			}
			buf.WriteByte(val)
		case 'c':
			return buf.String()
		default:
			buf.WriteByte('\\')
			buf.WriteByte(s[i])
		}
		i++
	}
	return buf.String()
}
