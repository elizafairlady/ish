package core

import "fmt"

var ErrReturn = fmt.Errorf("return")
var ErrBreak = fmt.Errorf("break")
var ErrContinue = fmt.Errorf("continue")
var ErrSetE = fmt.Errorf("set -e exit")
var ErrExit = fmt.Errorf("exit")
