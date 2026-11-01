package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentOptionalFilesAndPrecedence(t *testing.T) {
	const fallback = "MSERP_TEST_ENV_FALLBACK"
	const priority = "MSERP_TEST_ENV_PRIORITY"
	const process = "MSERP_TEST_ENV_PROCESS"
	for _, name := range []string{fallback, priority} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(process, "process")
	dir := t.TempDir()
	local, production := filepath.Join(dir, "local.env"), filepath.Join(dir, "production.env")
	if err := os.WriteFile(local, []byte(priority+"=local\n"+process+"=file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(production, []byte(fallback+"=production\n"+priority+"=production\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loadEnvironmentFiles(filepath.Join(dir, "missing.env"), local, production)
	for name, want := range map[string]string{fallback: "production", priority: "local", process: "process"} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
