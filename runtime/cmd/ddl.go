package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/openfoundry/runtime/bootstrap"
)

func ddl(dialect, output string, execute, force bool) error {
	stmts, err := bootstrap.MappedDDL(conf, dialect, force)
	if err != nil {
		slog.Error("print ddl failed", "error", err)
		return err
	}
	if err := writeDDL(stmts, output); err != nil {
		slog.Error("write ddl failed", "error", err, "output", output)
		return err
	}
	if !execute {
		return nil
	}
	if err := bootstrap.ExecStatements(conf, dialect, stmts); err != nil {
		slog.Error("exec ddl failed", "error", err)
		return err
	}
	return nil
}

func writeDDL(stmts []string, output string) error {
	rendered := make([]string, 0, len(stmts))
	for _, s := range stmts {
		s = withSQLSemi(s)
		if s == "" {
			continue
		}
		rendered = append(rendered, s)
	}
	if output == "" {
		for _, s := range rendered {
			fmt.Println(s)
		}
		return nil
	}
	path, err := resolveOutputPath(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := strings.Join(rendered, "\n")
	if body != "" {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func withSQLSemi(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" || strings.HasSuffix(s, ";") {
		return s
	}
	return s + ";"
}

func resolveOutputPath(output string) (string, error) {
	if filepath.IsAbs(output) {
		return output, nil
	}
	if wd, err := os.Getwd(); err == nil {
		if st, statErr := os.Stat(wd); statErr == nil && st.IsDir() {
			return filepath.Join(wd, output), nil
		}
	}
	if conf != nil && conf.BaseDir != "" {
		return filepath.Join(conf.BaseDir, output), nil
	}
	return filepath.Abs(output)
}
