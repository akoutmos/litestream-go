package main_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	main "github.com/benbjohnson/litestream/cmd/litestream"
)

// withStdin replaces os.Stdin with a pipe containing the given content
// for the duration of the callback. It restores os.Stdin afterward.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	go func() {
		defer w.Close()
		io.WriteString(w, content)
	}()

	os.Stdin = r
	fn()
}

// TestReadConfig tests the new ReadConfig helper function that routes
// between stdin and file-based config loading.
func TestReadConfig(t *testing.T) {
	t.Run("FromStdin", func(t *testing.T) {
		yaml := `
dbs:
  - path: /tmp/test-stdin.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			config, err := main.ReadConfig("", true, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(config.DBs) != 1 {
				t.Fatalf("expected 1 db, got %d", len(config.DBs))
			}
			if config.DBs[0].Path != "/tmp/test-stdin.db" {
				t.Errorf("expected path %q, got %q", "/tmp/test-stdin.db", config.DBs[0].Path)
			}
		})
	})

	t.Run("FromStdinWithEnvExpansion", func(t *testing.T) {
		t.Setenv("TEST_STDIN_DB_PATH", "/tmp/env-expanded.db")

		yaml := `
dbs:
  - path: $TEST_STDIN_DB_PATH
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			config, err := main.ReadConfig("", true, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(config.DBs) != 1 {
				t.Fatalf("expected 1 db, got %d", len(config.DBs))
			}
			if config.DBs[0].Path != "/tmp/env-expanded.db" {
				t.Errorf("expected path %q, got %q", "/tmp/env-expanded.db", config.DBs[0].Path)
			}
		})
	})

	t.Run("FromStdinWithoutEnvExpansion", func(t *testing.T) {
		t.Setenv("TEST_STDIN_NOEXPAND", "/should/not/appear")

		yaml := `
dbs:
  - path: $TEST_STDIN_NOEXPAND
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			config, err := main.ReadConfig("", true, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(config.DBs) != 1 {
				t.Fatalf("expected 1 db, got %d", len(config.DBs))
			}
			// With expansion disabled, the literal string should be preserved.
			if config.DBs[0].Path != "$TEST_STDIN_NOEXPAND" {
				t.Errorf("expected literal %q, got %q", "$TEST_STDIN_NOEXPAND", config.DBs[0].Path)
			}
		})
	})

	t.Run("ErrorBothConfigAndStdin", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			_, err := main.ReadConfig("/some/path.yml", true, true)
			if err == nil {
				t.Fatal("expected error when both config path and stdin are specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})

	t.Run("FromFile", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "litestream.yml")
		yaml := `
dbs:
  - path: /tmp/from-file.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := main.ReadConfig(configPath, false, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(config.DBs) != 1 {
			t.Fatalf("expected 1 db, got %d", len(config.DBs))
		}
		if config.DBs[0].Path != "/tmp/from-file.db" {
			t.Errorf("expected path %q, got %q", "/tmp/from-file.db", config.DBs[0].Path)
		}
	})

	t.Run("DefaultPathWhenEmpty", func(t *testing.T) {
		// When both configPath and fromStdin are false/empty, ReadConfig
		// falls back to DefaultConfigPath(). This will likely fail because
		// the default config file doesn't exist in test environments, but
		// the error should be about a missing file, not a logic error.
		_, err := main.ReadConfig("", false, true)
		if err == nil {
			// If it succeeds, the default config file exists — that's fine.
			return
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected 'config file not found' error for default path, got: %v", err)
		}
	})

	t.Run("FromStdinWithInvalidYAML", func(t *testing.T) {
		withStdin(t, "{{invalid yaml: [}", func() {
			_, err := main.ReadConfig("", true, true)
			if err == nil {
				t.Fatal("expected error for invalid YAML")
			}
		})
	})

	t.Run("FromStdinEmpty", func(t *testing.T) {
		// An empty stdin should parse successfully into a default config
		// (no databases configured).
		withStdin(t, "", func() {
			config, err := main.ReadConfig("", true, true)
			if err != nil {
				t.Fatalf("unexpected error for empty stdin: %v", err)
			}
			if len(config.DBs) != 0 {
				t.Errorf("expected 0 dbs from empty stdin, got %d", len(config.DBs))
			}
		})
	})
}

// TestReplicateCommand_ParseFlags_Stdin tests the --stdin flag on the replicate command.
func TestReplicateCommand_ParseFlags_Stdin(t *testing.T) {
	t.Run("StdinLoadsConfig", func(t *testing.T) {
		yaml := `
dbs:
  - path: /tmp/stdin-replicate.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cmd.Config.DBs) != 1 {
				t.Fatalf("expected 1 db, got %d", len(cmd.Config.DBs))
			}
			if cmd.Config.DBs[0].Path != "/tmp/stdin-replicate.db" {
				t.Errorf("expected path %q, got %q", "/tmp/stdin-replicate.db", cmd.Config.DBs[0].Path)
			}
		})
	})

	t.Run("StdinConflictsWithPositionalArgs", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin", "test.db", "file:///tmp/replica"})
			if err == nil {
				t.Fatal("expected error when -stdin is used with positional args")
			}
			if !strings.Contains(err.Error(), "-stdin") {
				t.Errorf("expected error mentioning '-stdin', got: %v", err)
			}
		})
	})

	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin", "-config", "/some/path.yml"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})

	t.Run("StdinWithLogLevel", func(t *testing.T) {
		yaml := `
dbs:
  - path: /tmp/stdin-loglevel.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin", "-log-level", "debug"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Config.Logging.Level != "debug" {
				t.Errorf("expected log level %q, got %q", "debug", cmd.Config.Logging.Level)
			}
		})
	})

	t.Run("StdinWithExec", func(t *testing.T) {
		yaml := `
dbs:
  - path: /tmp/stdin-exec.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin", "-exec", "echo hello"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Config.Exec != "echo hello" {
				t.Errorf("expected exec %q, got %q", "echo hello", cmd.Config.Exec)
			}
		})
	})

	t.Run("StdinConfigPathIsEmpty", func(t *testing.T) {
		yaml := `
dbs:
  - path: /tmp/stdin-nopath.db
    replicas:
      - url: file:///tmp/replica
`[1:]
		withStdin(t, yaml, func() {
			cmd := main.NewReplicateCommand()
			err := cmd.ParseFlags(context.Background(), []string{"-stdin"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// When using stdin, ConfigPath should be empty since there's no file.
			if cmd.Config.ConfigPath != "" {
				t.Errorf("expected empty ConfigPath when using stdin, got %q", cmd.Config.ConfigPath)
			}
		})
	})
}

// TestDatabasesCommand_Stdin tests the --stdin flag on the databases command.
func TestDatabasesCommand_Stdin(t *testing.T) {
	t.Run("StdinLoadsConfig", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		yaml := `
dbs:
  - path: ` + dbPath + `
    replicas:
      - url: file://` + filepath.Join(dir, "replica") + `
`[1:]
		withStdin(t, yaml, func() {
			cmd := &main.DatabasesCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	})

	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.DatabasesCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "-config", "/some/path.yml"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})
}

// TestStatusCommand_Stdin tests the --stdin flag on the status command.
func TestStatusCommand_Stdin(t *testing.T) {
	t.Run("StdinLoadsConfig", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		// Create an empty file so status doesn't error on missing db.
		if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}

		yaml := `
dbs:
  - path: ` + dbPath + `
    replicas:
      - url: file://` + filepath.Join(dir, "replica") + `
`[1:]
		withStdin(t, yaml, func() {
			cmd := &main.StatusCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	})

	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.StatusCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "-config", "/some/path.yml"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})
}

// TestRestoreCommand_Stdin tests the --stdin flag on the restore command.
func TestRestoreCommand_Stdin(t *testing.T) {
	t.Run("StdinConflictsWithReplicaURL", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.RestoreCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "s3://mybucket/db"})
			if err == nil {
				t.Fatal("expected error when -stdin is used with a replica URL")
			}
			if !strings.Contains(err.Error(), "-stdin") {
				t.Errorf("expected error mentioning '-stdin', got: %v", err)
			}
		})
	})

	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.RestoreCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "-config", "/some/path.yml", "/tmp/test.db"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})

	t.Run("StdinWithDBPath", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		yaml := `
dbs:
  - path: ` + dbPath + `
    replicas:
      - url: file://` + filepath.Join(dir, "replica") + `
`[1:]
		withStdin(t, yaml, func() {
			cmd := &main.RestoreCommand{}
			// This will fail because the replica doesn't exist, but the
			// error should be about the replica, not about config loading.
			err := cmd.Run(context.Background(), []string{"-stdin", dbPath})
			if err == nil {
				return // Unexpected success is fine if things happen to line up.
			}
			// Should NOT be a config loading error.
			if strings.Contains(err.Error(), "cannot specify both") ||
				strings.Contains(err.Error(), "config file not found") {
				t.Errorf("unexpected config error, expected replica/db error, got: %v", err)
			}
		})
	})
}

// TestLTXCommand_Stdin tests the --stdin flag on the ltx command.
func TestLTXCommand_Stdin(t *testing.T) {
	t.Run("StdinConflictsWithReplicaURL", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.LTXCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "s3://mybucket/db"})
			if err == nil {
				t.Fatal("expected error when -stdin is used with a replica URL")
			}
			if !strings.Contains(err.Error(), "-stdin") {
				t.Errorf("expected error mentioning '-stdin', got: %v", err)
			}
		})
	})

	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.LTXCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "-config", "/some/path.yml", "/tmp/test.db"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})

	t.Run("StdinWithDBPath", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		yaml := `
dbs:
  - path: ` + dbPath + `
    replicas:
      - url: file://` + filepath.Join(dir, "replica") + `
`[1:]
		withStdin(t, yaml, func() {
			cmd := &main.LTXCommand{}
			// This will fail because the db/replica doesn't exist, but the
			// error should be about the missing db, not about config loading.
			err := cmd.Run(context.Background(), []string{"-stdin", dbPath})
			if err == nil {
				return
			}
			if strings.Contains(err.Error(), "cannot specify both") ||
				strings.Contains(err.Error(), "config file not found") {
				t.Errorf("unexpected config error, expected db/replica error, got: %v", err)
			}
		})
	})
}

// TestResetCommand_Stdin tests the --stdin flag on the reset command.
func TestResetCommand_Stdin(t *testing.T) {
	t.Run("StdinConflictsWithConfig", func(t *testing.T) {
		withStdin(t, "dbs: []\n", func() {
			cmd := &main.ResetCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", "-config", "/some/path.yml", "/tmp/test.db"})
			if err == nil {
				t.Fatal("expected error when -stdin and -config are both specified")
			}
			if !strings.Contains(err.Error(), "cannot specify both") {
				t.Errorf("expected 'cannot specify both' error, got: %v", err)
			}
		})
	})

	t.Run("StdinWithDBPath", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")

		// Create a real file so the reset command can find it.
		if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}

		yaml := `
dbs:
  - path: ` + dbPath + `
    replicas:
      - url: file://` + filepath.Join(dir, "replica") + `
`[1:]
		withStdin(t, yaml, func() {
			cmd := &main.ResetCommand{}
			err := cmd.Run(context.Background(), []string{"-stdin", dbPath})
			// The reset command may succeed (no local state to reset) or fail
			// for a db-related reason, but should NOT fail with a config error.
			if err != nil &&
				(strings.Contains(err.Error(), "cannot specify both") ||
					strings.Contains(err.Error(), "config file not found")) {
				t.Errorf("unexpected config error, got: %v", err)
			}
		})
	})
}
