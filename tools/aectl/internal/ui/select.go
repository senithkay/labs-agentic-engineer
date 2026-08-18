// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// SelectItem is one entry shown in a MultiSelect list.
type SelectItem struct {
	Label       string
	Description string
}

// MultiSelect renders an interactive checkbox list.
// Returns (selected []bool, confirmed bool).
// confirmed is false when the user presses Esc — nothing should be applied.
// Falls back to a numbered prompt when stdout is not a TTY.
func MultiSelect(title string, items []SelectItem) (selected []bool, confirmed bool) {
	selected = make([]bool, len(items))
	if !isTTY || len(items) == 0 {
		return multiSelectFallback(title, items, selected)
	}

	cursor := 0

	// render redraws all item rows + the hint footer.
	// On the first call (initial=true) the cursor is already after the title line;
	// on subsequent calls we move the cursor back up to overwrite.
	render := func(initial bool) {
		if !initial {
			// Move up past: blank(1) + items(N) + hint(1) = N+2 lines.
			fmt.Printf("\033[%dA", len(items)+2)
		}
		fmt.Print("\r\033[K\n") // blank separator line
		for i, item := range items {
			var check string
			if selected[i] {
				check = colorize(ansiOrange, "[✓]")
			} else {
				check = colorize(ansiGray, "[ ]")
			}
			marker := "  "
			if i == cursor {
				marker = colorize(ansiOrange, "▶ ")
			}
			fmt.Printf("\r\033[K  %s%s %-20s%s\n",
				marker, check, item.Label, colorize(ansiGray, item.Description))
		}
		fmt.Printf("\r\033[K  %s\n",
			colorize(ansiGray, "↑↓ navigate   space select   enter install   esc skip"))
	}

	fmt.Printf("\n  %s\n", colorize(ansiGray, title))
	render(true)

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return multiSelectFallback(title, items, selected)
	}

	restore := func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			restore()
			fmt.Println()
			return selected, true
		}
		b := buf[:n]

		switch {
		case n == 1 && b[0] == 27: // bare Esc
			restore()
			fmt.Println()
			return selected, false
		case n == 1 && (b[0] == 13 || b[0] == 10): // Enter / Return
			restore()
			fmt.Println()
			return selected, true
		case n == 1 && b[0] == 3: // Ctrl+C
			restore()
			fmt.Println()
			os.Exit(1)
		case n == 1 && b[0] == ' ':
			selected[cursor] = !selected[cursor]
		case n >= 3 && b[0] == 27 && b[1] == '[' && b[2] == 'A': // ↑
			if cursor > 0 {
				cursor--
			}
		case n >= 3 && b[0] == 27 && b[1] == '[' && b[2] == 'B': // ↓
			if cursor < len(items)-1 {
				cursor++
			}
		}
		render(false)
	}
}

func multiSelectFallback(title string, items []SelectItem, selected []bool) ([]bool, bool) {
	fmt.Printf("\n  %s\n\n", title)
	for i, item := range items {
		fmt.Printf("  [%d] %s — %s\n", i+1, item.Label, item.Description)
	}
	fmt.Print("\n  Enter numbers to install (e.g. 1,2) or blank to skip: ")
	var line string
	fmt.Scanln(&line) //nolint:errcheck
	line = strings.TrimSpace(line)
	if line == "" {
		return selected, false
	}
	for _, part := range strings.Split(line, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && n >= 1 && n <= len(items) {
			selected[n-1] = true
		}
	}
	return selected, true
}
