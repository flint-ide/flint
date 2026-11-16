package sandbox

import (
	"bufio"
	"debug/elf"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Interpreters lists the programs the system runs in order to run program:
// a script's "#!" interpreter (through env, the command it names, looked up
// in pathEnv) and an ELF program's loader, followed down to a program that
// needs none. They are what a sandboxed extension that may not start other
// programs must still be allowed to execute. Paths are returned with
// symbolic links resolved; what can't be read or found is left out (the
// program then fails to start, and says so).
func Interpreters(program, pathEnv string) []string {
	var out []string
	queue := []string{program}
	for depth := 0; len(queue) > 0 && depth < 8; depth++ {
		file := queue[0]
		queue = queue[1:]
		for _, next := range interpretersOf(file, pathEnv) {
			real, err := filepath.EvalSymlinks(next)
			if err != nil || real == program || slices.Contains(out, real) {
				continue
			}
			out = append(out, real)
			queue = append(queue, real)
		}
	}
	return out
}

// interpretersOf is one step: the interpreter of a script (and the command
// env runs), or the loader of an ELF program.
func interpretersOf(file, pathEnv string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	head, _ := bufio.NewReaderSize(f, 512).Peek(512) // the kernel reads at most 256
	if line, ok := strings.CutPrefix(string(head), "#!"); ok {
		line, _, _ = strings.Cut(line, "\n")
		fields := strings.Fields(line)
		if len(fields) == 0 || !filepath.IsAbs(fields[0]) {
			return nil
		}
		out := []string{fields[0]}
		if filepath.Base(fields[0]) == "env" {
			for _, arg := range fields[1:] {
				if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
					continue // env's options and assignments
				}
				if found := lookPath(arg, pathEnv); found != "" {
					out = append(out, found)
				}
				break
			}
		}
		return out
	}
	ef, err := elf.NewFile(f)
	if err != nil {
		return nil // not ELF (macOS: the loader is the kernel's business)
	}
	for _, prog := range ef.Progs {
		if prog.Type != elf.PT_INTERP || prog.Filesz == 0 || prog.Filesz > 4096 {
			continue
		}
		data := make([]byte, prog.Filesz)
		if _, err := prog.ReadAt(data, 0); err != nil {
			return nil
		}
		if interp := strings.TrimRight(string(data), "\x00"); filepath.IsAbs(interp) {
			return []string{interp}
		}
	}
	return nil
}

// lookPath finds an executable file name in the folders of pathEnv.
func lookPath(name, pathEnv string) string {
	if filepath.IsAbs(name) {
		return name
	}
	if strings.ContainsRune(name, '/') {
		return ""
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		file := filepath.Join(dir, name)
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return file
		}
	}
	return ""
}
