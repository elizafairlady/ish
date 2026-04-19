package builtin

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/term"

	"ish/internal/core"
)

func builtinTest(args []string, env *core.Env) (int, error) {
	if len(args) > 0 && args[len(args)-1] == "]" {
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		return 1, nil
	}
	pos := 0
	result := evalTestOr(args, &pos)
	if pos != len(args) {
		return 2, fmt.Errorf("test: unexpected argument: %s", args[pos])
	}
	if result {
		return 0, nil
	}
	return 1, nil
}

func evalTestOr(args []string, pos *int) bool {
	result := evalTestAnd(args, pos)
	for *pos < len(args) && args[*pos] == "-o" {
		*pos++
		right := evalTestAnd(args, pos)
		result = result || right
	}
	return result
}

func evalTestAnd(args []string, pos *int) bool {
	result := evalTestNot(args, pos)
	for *pos < len(args) && args[*pos] == "-a" {
		*pos++
		right := evalTestNot(args, pos)
		result = result && right
	}
	return result
}

func evalTestNot(args []string, pos *int) bool {
	if *pos < len(args) && args[*pos] == "!" {
		*pos++
		return !evalTestNot(args, pos)
	}
	return evalTestPrimary(args, pos)
}

func evalTestPrimary(args []string, pos *int) bool {
	if *pos >= len(args) {
		return false
	}

	if args[*pos] == "(" {
		*pos++
		result := evalTestOr(args, pos)
		if *pos < len(args) && args[*pos] == ")" {
			*pos++
		}
		return result
	}

	if *pos+2 < len(args) && isBinaryOp(args[*pos+1]) {
		return evalBinaryOp(args, pos)
	}

	if *pos+1 < len(args) {
		op := args[*pos]
		switch op {
		case "-n":
			*pos++
			operand := args[*pos]
			*pos++
			return operand != ""
		case "-z":
			*pos++
			operand := args[*pos]
			*pos++
			return operand == ""
		case "-f":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode().IsRegular()
		case "-d":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.IsDir()
		case "-e":
			*pos++
			operand := args[*pos]
			*pos++
			_, err := os.Stat(operand)
			return err == nil
		case "-s":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Size() > 0
		case "-r":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x4) == nil
		case "-w":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x2) == nil
		case "-x":
			*pos++
			operand := args[*pos]
			*pos++
			return syscall.Access(operand, 0x1) == nil
		case "-L", "-h":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Lstat(operand)
			return err == nil && info.Mode()&os.ModeSymlink != 0
		case "-p":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode()&os.ModeNamedPipe != 0
		case "-S":
			*pos++
			operand := args[*pos]
			*pos++
			info, err := os.Stat(operand)
			return err == nil && info.Mode()&os.ModeSocket != 0
		case "-t":
			*pos++
			operand := args[*pos]
			*pos++
			fd, err := strconv.Atoi(operand)
			if err != nil {
				return false
			}
			return term.IsTerminal(fd)
		}
	}

	operand := args[*pos]
	*pos++

	if *pos+1 < len(args) && isBinaryOp(args[*pos]) {
		return evalBinaryOpWith(operand, args, pos)
	}

	return operand != ""
}

func isBinaryOp(s string) bool {
	switch s {
	case "=", "==", "!=", "-eq", "-ne", "-lt", "-le", "-gt", "-ge":
		return true
	}
	return false
}

func evalBinaryOp(args []string, pos *int) bool {
	operand := args[*pos]
	*pos++
	return evalBinaryOpWith(operand, args, pos)
}

func evalBinaryOpWith(operand string, args []string, pos *int) bool {
	op := args[*pos]
	*pos++
	right := args[*pos]
	*pos++
	switch op {
	case "=", "==":
		return operand == right
	case "!=":
		return operand != right
	case "-eq":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a == b })
	case "-ne":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a != b })
	case "-lt":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a < b })
	case "-le":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a <= b })
	case "-gt":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a > b })
	case "-ge":
		return testNumCmpBool(operand, right, func(a, b int) bool { return a >= b })
	}
	return false
}

func testNumCmpBool(a, b string, cmp func(int, int) bool) bool {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return cmp(ai, bi)
}
