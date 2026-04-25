package eval

import (
	"fmt"
	"testing"

	"ish/internal/value"
)

func TestDebugFlattenConflict(t *testing.T) {
	_ = value.Nil
	env := NewEnv()

	// Does List.reverse work at all?
	out := capture(env, func() {
		Run(`echo $(List.reverse([3,2,1]))`, env)
	})
	fmt.Printf("List.reverse: %q\n", out)

	// Does nested call work?
	out2 := capture(env, func() {
		Run(`echo $(List.reverse(List.map([1,2,3], \x -> x * 10)))`, env)
	})
	fmt.Printf("reverse(map(...)): %q\n", out2)

	// Does it work inside a named fn?
	Run(`fn test6 do
  List.reverse(List.map([1,2,3], \x -> x * 10))
end`, env)
	out3 := capture(env, func() {
		Run(`echo $(test6)`, env)
	})
	fmt.Printf("test6 (no params): %q\n", out3)

	// With params?
	Run(`fn test7 list do
  List.reverse(List.map(list, \x -> x * 10))
end`, env)
	out4 := capture(env, func() {
		Run(`echo $(test7([1,2,3]))`, env)
	})
	fmt.Printf("test7 (one param): %q\n", out4)

	// Two params?
	Run(`fn test8 list, f do
  List.reverse(List.map(list, f))
end`, env)
	out5 := capture(env, func() {
		Run(`echo $(test8([1,2,3], \x -> x * 10))`, env)
	})
	fmt.Printf("test8 (two params): %q\n", out5)
}
