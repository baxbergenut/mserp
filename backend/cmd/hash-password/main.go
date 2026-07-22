package main

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	password := readPassword("Password: ")
	confirmation := readPassword("Confirm password: ")
	if !bytes.Equal(password, confirmation) {
		fmt.Fprintln(os.Stderr, "passwords do not match")
		os.Exit(1)
	}
	if len(password) < 12 {
		fmt.Fprintln(os.Stderr, "password must be at least 12 characters")
		os.Exit(1)
	}
	if len(password) > 72 {
		fmt.Fprintln(os.Stderr, "password must be at most 72 bytes")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword(password, 12)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash password:", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}

func readPassword(prompt string) []byte {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read password:", err)
		os.Exit(1)
	}
	return value
}
