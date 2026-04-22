package parser

// SymKind identifies what a name resolves to at parse time.
type SymKind byte

const (
	SymBuiltin SymKind = iota + 1 // shell builtin (echo, cd, export)
	SymModule                     // module with functions (String, List, Enum)
	SymFn                         // ish function (fn name, Kernel auto-import) — value args, NCall
	SymPOSIXFn                    // POSIX function (name() { }) — string args, NCmd
	SymVar                        // bound variable (name = expr, not known to be a function)
)

// Symbol is a parser symbol table entry.
type Symbol struct {
	Kind SymKind
	Fns  map[string]bool // function names (only for SymModule)
}
