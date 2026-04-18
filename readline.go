package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// CompleteFn returns completion candidates for a word prefix.
// isFirst is true if the word is at command position (first word on the line).
type CompleteFn func(prefix string, isFirst bool) []string

// Readline provides line editing with history for the interactive REPL.
type Readline struct {
	history   []string
	histFile  string
	Complete  CompleteFn // set by the shell for tab completion
	maxHist  int
}

func NewReadline() *Readline {
	rl := &Readline{maxHist: 1000}
	home := os.Getenv("HOME")
	if home != "" {
		rl.histFile = home + "/.ish_history"
		rl.loadHistory()
	}
	return rl
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

func (rl *Readline) loadHistory() {
	data, err := os.ReadFile(rl.histFile)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > rl.maxHist {
		lines = lines[len(lines)-rl.maxHist:]
	}
	rl.history = lines
}

func (rl *Readline) saveHistory() {
	if rl.histFile == "" {
		return
	}
	lines := rl.history
	if len(lines) > rl.maxHist {
		lines = lines[len(lines)-rl.maxHist:]
	}
	if err := os.WriteFile(rl.histFile, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "ish: warning: could not save history: %s\n", err)
	}
}

func (rl *Readline) addHistory(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	// Don't duplicate consecutive entries
	if len(rl.history) > 0 && rl.history[len(rl.history)-1] == line {
		return
	}
	rl.history = append(rl.history, line)
	rl.saveHistory()
}

// ReadLine reads a line with editing support. Returns the line and false on EOF.
func (rl *Readline) ReadLine(prompt string) (string, bool) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Not a terminal — fall back to simple reading
		return rl.readSimple(prompt)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return rl.readSimple(prompt)
	}
	defer term.Restore(fd, oldState)

	line, ok := rl.readRaw(fd, prompt)
	if ok {
		// Print newline after the line is submitted
		fmt.Fprint(os.Stderr, "\r\n")
	}
	return line, ok
}

func (rl *Readline) readSimple(prompt string) (string, bool) {
	fmt.Fprint(os.Stderr, prompt)
	var buf [4096]byte
	n, err := os.Stdin.Read(buf[:])
	if n == 0 && err != nil {
		return "", false
	}
	return strings.TrimRight(string(buf[:n]), "\n\r"), true
}

func (rl *Readline) readRaw(fd int, prompt string) (string, bool) {
	var line []rune
	pos := 0 // cursor position within line (rune index)
	histIdx := len(rl.history)
	var savedLine []rune // line saved when browsing history

	redraw := func() {
		// Clear line: move to start, clear to end, print prompt + line, position cursor
		fmt.Fprintf(os.Stderr, "\r\033[K%s%s", prompt, string(line))
		// Move cursor to correct position
		if pos < len(line) {
			fmt.Fprintf(os.Stderr, "\033[%dD", len(line)-pos)
		}
	}

	redraw()

	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if n == 0 && err != nil {
			return "", false
		}

		for i := 0; i < n; i++ {
			ch := buf[i]

			switch {
			case ch == '\r' || ch == '\n':
				return string(line), true

			case ch == 4: // Ctrl-D
				if len(line) == 0 {
					return "", false
				}
				// Delete char at cursor
				if pos < len(line) {
					line = append(line[:pos], line[pos+1:]...)
					redraw()
				}

			case ch == 3: // Ctrl-C
				line = nil
				pos = 0
				fmt.Fprint(os.Stderr, "^C\r\n")
				redraw()

			case ch == 127 || ch == 8: // Backspace
				if pos > 0 {
					line = append(line[:pos-1], line[pos:]...)
					pos--
					redraw()
				}

			case ch == 1: // Ctrl-A (home)
				pos = 0
				redraw()

			case ch == 5: // Ctrl-E (end)
				pos = len(line)
				redraw()

			case ch == 11: // Ctrl-K (kill to end)
				line = line[:pos]
				redraw()

			case ch == 21: // Ctrl-U (kill to start)
				line = line[pos:]
				pos = 0
				redraw()

			case ch == 23: // Ctrl-W (kill word back)
				if pos > 0 {
					end := pos
					for pos > 0 && line[pos-1] == ' ' {
						pos--
					}
					for pos > 0 && line[pos-1] != ' ' {
						pos--
					}
					line = append(line[:pos], line[end:]...)
					redraw()
				}

			case ch == 12: // Ctrl-L (clear screen)
				fmt.Fprint(os.Stderr, "\033[2J\033[H")
				redraw()

			case ch == 27: // ESC — start of escape sequence
				if i+1 < n && buf[i+1] == '[' {
					i++
					if i+1 < n {
						i++
						switch buf[i] {
						case 'A': // Up
							if histIdx > 0 {
								if histIdx == len(rl.history) {
									savedLine = make([]rune, len(line))
									copy(savedLine, line)
								}
								histIdx--
								line = []rune(rl.history[histIdx])
								pos = len(line)
								redraw()
							}
						case 'B': // Down
							if histIdx < len(rl.history) {
								histIdx++
								if histIdx == len(rl.history) {
									line = savedLine
								} else {
									line = []rune(rl.history[histIdx])
								}
								pos = len(line)
								redraw()
							}
						case 'C': // Right
							if pos < len(line) {
								pos++
								redraw()
							}
						case 'D': // Left
							if pos > 0 {
								pos--
								redraw()
							}
						case 'H': // Home
							pos = 0
							redraw()
						case 'F': // End
							pos = len(line)
							redraw()
						case '3': // Delete key (ESC [ 3 ~)
							if i+1 < n && buf[i+1] == '~' {
								i++
								if pos < len(line) {
									line = append(line[:pos], line[pos+1:]...)
									redraw()
								}
							}
						}
					}
				}

			case ch == '\t': // Tab completion
				if rl.Complete == nil {
					// No completer — insert spaces
					spaces := []rune("    ")
					line = append(line[:pos], append(spaces, line[pos:]...)...)
					pos += 4
					redraw()
				} else {
					// Extract the word being completed
					lineStr := string(line[:pos])
					wordStart := strings.LastIndexAny(lineStr, " \t") + 1
					prefix := lineStr[wordStart:]
					isFirst := strings.TrimSpace(lineStr[:wordStart]) == ""
					candidates := rl.Complete(prefix, isFirst)

					if len(candidates) == 1 {
						// Single match — complete it
						completion := []rune(candidates[0][len(prefix):])
						// Add trailing space if not a directory
						if !strings.HasSuffix(candidates[0], "/") {
							completion = append(completion, ' ')
						}
						line = append(line[:pos], append(completion, line[pos:]...)...)
						pos += len(completion)
						redraw()
					} else if len(candidates) > 1 {
						// Multiple matches — find common prefix and show options
						common := longestCommonPrefix(candidates)
						if len(common) > len(prefix) {
							completion := []rune(common[len(prefix):])
							line = append(line[:pos], append(completion, line[pos:]...)...)
							pos += len(completion)
						}
						// Print candidates below the prompt
						fmt.Fprint(os.Stderr, "\r\n")
						for _, c := range candidates {
							fmt.Fprintf(os.Stderr, "%s  ", c)
						}
						fmt.Fprint(os.Stderr, "\r\n")
						redraw()
					}
				}

			case ch >= 32: // Printable character
				if ch < 0x80 {
					// ASCII
					line = append(line[:pos], append([]rune{rune(ch)}, line[pos:]...)...)
					pos++
				} else {
					// Start of UTF-8 sequence — collect all bytes
					utf8Buf := []byte{ch}
					// Determine how many continuation bytes we need
					needed := 0
					if ch&0xE0 == 0xC0 {
						needed = 1
					}
					if ch&0xF0 == 0xE0 {
						needed = 2
					}
					if ch&0xF8 == 0xF0 {
						needed = 3
					}
					for j := 0; j < needed && i+1 < n; j++ {
						i++
						utf8Buf = append(utf8Buf, buf[i])
					}
					r, _ := utf8.DecodeRune(utf8Buf)
					if r != utf8.RuneError {
						line = append(line[:pos], append([]rune{r}, line[pos:]...)...)
						pos++
					}
				}
				redraw()
			}
		}
	}
}
