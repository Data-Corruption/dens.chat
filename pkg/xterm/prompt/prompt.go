// Package prompt provides functions for asking interactive questions in the terminal.
//
// Available functions:
//   - [Secret] Reads a line without terminal echo.
//   - [YesNo]  Asks a yes/no question; returns true when the answer is “yes”.
//   - [Line]   Reads a line of text, without the spaces around it.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// exported

// Secret prompts on stderr and reads a secret from stdin. Terminal input is
// read without echo; piped input reads one line.
func Secret(p string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return secretR(os.Stdin, os.Stderr, p)
	}

	fmt.Fprintf(os.Stderr, "%s: ", p)
	value, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // ReadPassword does not echo the newline
	if err != nil {
		return "", fmt.Errorf("error reading secret: %w", err)
	}
	return string(value), nil
}

// YesNo asks a yes/no question to the user until a (y/n) response is given or an error occurs.
func YesNo(p string) (bool, error) { return yesNoR(os.Stdin, p) }

// Line prompts on stdout and reads a line of text from stdin, without the
// spaces around it.
func Line(p string) (string, error) { return lineR(os.Stdin, os.Stdout, p) }

// internal

func secretR(r io.Reader, w io.Writer, prompt string) (string, error) {
	fmt.Fprintf(w, "%s: ", prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("error reading secret: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func lineR(r io.Reader, w io.Writer, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("error reading input: %w", err)
	}
	// Windows PowerShell 5.1 starts text it pipes to a program with a byte
	// order mark when $OutputEncoding is UTF-8.
	return strings.TrimSpace(strings.TrimPrefix(line, "\ufeff")), nil
}

func yesNoR(r io.Reader, prompt string) (bool, error) {
	reader := bufio.NewReader(r)
	fullPrompt := fmt.Sprintf("%s (y/n): ", prompt)
	// loop until valid input is received
	for {
		fmt.Print(fullPrompt)
		input, err := readLine(reader)
		if err != nil && err != io.EOF {
			return false, fmt.Errorf("error reading input: %w", err)
		}
		if err == io.EOF && input == "" {
			return false, fmt.Errorf("error reading input: %w", io.EOF)
		}
		// handle input
		switch strings.ToLower(input) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("Invalid input. Please enter one of: 'y', 'yes', 'n', or 'no'.")
		}
	}
}

// Helper function to read a line from stdin
func readLine(reader *bufio.Reader) (string, error) {
	str, err := reader.ReadString('\n')
	return strings.TrimSpace(str), err
}
