package stdlib

import (
	"math"

	"ish/internal/value"
)

func mathModule() *value.OrdMap {
	m := makeModule(map[string]func([]value.Value) (value.Value, error){
		"abs": func(args []value.Value) (value.Value, error) {
			a := arg(args, 0)
			if a.Kind == value.VInt {
				n := a.Int()
				if n < 0 {
					n = -n
				}
				return value.IntVal(n), nil
			}
			return value.FloatVal(math.Abs(toF(a))), nil
		},
		"sqrt":  func(args []value.Value) (value.Value, error) { return value.FloatVal(math.Sqrt(toF(arg(args, 0)))), nil },
		"pow":   func(args []value.Value) (value.Value, error) { return value.FloatVal(math.Pow(toF(arg(args, 0)), toF(arg(args, 1)))), nil },
		"floor": func(args []value.Value) (value.Value, error) { return value.IntVal(int64(math.Floor(toF(arg(args, 0))))), nil },
		"ceil":  func(args []value.Value) (value.Value, error) { return value.IntVal(int64(math.Ceil(toF(arg(args, 0))))), nil },
		"round": func(args []value.Value) (value.Value, error) { return value.IntVal(int64(math.Round(toF(arg(args, 0))))), nil },
		"log": func(args []value.Value) (value.Value, error) { return value.FloatVal(math.Log(toF(arg(args, 0)))), nil },
		"min": func(args []value.Value) (value.Value, error) {
			a, b := arg(args, 0), arg(args, 1)
			if a.Kind == value.VInt && b.Kind == value.VInt {
				if a.Int() < b.Int() {
					return a, nil
				}
				return b, nil
			}
			if toF(a) < toF(b) {
				return a, nil
			}
			return b, nil
		},
		"max": func(args []value.Value) (value.Value, error) {
			a, b := arg(args, 0), arg(args, 1)
			if a.Kind == value.VInt && b.Kind == value.VInt {
				if a.Int() > b.Int() {
					return a, nil
				}
				return b, nil
			}
			if toF(a) > toF(b) {
				return a, nil
			}
			return b, nil
		},
	})
	m.Keys = append(m.Keys, "pi")
	m.Vals["pi"] = value.FloatVal(math.Pi)
	return m
}

func toF(v value.Value) float64 {
	if v.Kind == value.VInt {
		return float64(v.Int())
	}
	return v.Float()
}
