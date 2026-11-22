// Command lbug-setup warms the LadybugDB extension cache (INSTALL) and
// copies the engine extension libraries into third_party/ladybug-ext so
// the daemon never downloads at runtime (local-first). Run via
// `task setup:chitta`.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	lbug "github.com/LadybugDB/go-ladybug"
)

func main() {
	outDir := "third_party/ladybug-ext"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	scratch, err := os.MkdirTemp("", "lbug-setup-*")
	if err != nil {
		fatal(err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	db, err := lbug.OpenDatabase(filepath.Join(scratch, "db"), lbug.DefaultSystemConfig())
	if err != nil {
		fatal(err)
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		fatal(err)
	}
	for _, ext := range []string{"fts", "vector"} {
		if res, err := conn.Query("INSTALL " + ext); err != nil {
			fatal(fmt.Errorf("install %s: %w", ext, err))
		} else {
			res.Close()
		}
	}
	conn.Close()
	db.Close()
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, ".lbdb", "extension", "*", "*", "*", "lib*.lbug_extension"))
	if len(matches) == 0 {
		fatal(fmt.Errorf("no extension libraries found in engine cache"))
	}
	for _, m := range matches {
		base := filepath.Base(m)
		dst := filepath.Join(outDir, base)
		if err := copyFile(m, dst); err != nil {
			fatal(err)
		}
		fmt.Printf("vendored %s\n", dst)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "lbug-setup:", err)
	os.Exit(1)
}
