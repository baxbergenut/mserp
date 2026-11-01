package main

import "github.com/joho/godotenv"

func loadEnvironmentFiles(paths ...string) {
	// Optional local files must not prevent operator commands from reading the
	// production configuration. Process variables and earlier files still win.
	for _, path := range paths {
		_ = godotenv.Load(path)
	}
}
