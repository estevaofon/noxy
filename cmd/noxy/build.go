// cmd/noxy/build.go — `noxy build <entry.nx> [-o <saida>] [--include <p>]... [--list]`
// (spec 2026-09-26 §3.1). Flags podem vir depois do entry; o pacote flag
// para no primeiro positional, por isso o parser e manual.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/estevaofon/noxy/internal/build"
)

const buildUsage = `Usage: noxy build <entry.nx> [-o <output>] [--include <path>]... [--list]

  -o <output>       executable to write (default: the entry name without .nx; .exe is added on Windows)
  --include <path>  file or directory to embed, relative to the project root (repeatable; adds to
                    the "include" lines of noxy.mod)
  --list            print what would go into the payload and write nothing
`

type buildArgs struct {
	entry    string
	output   string
	includes []string
	list     bool
	help     bool
}

func parseBuildArgs(args []string) (buildArgs, error) {
	var out buildArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		takeValue := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "-o", "--o", "-output", "--output":
			v, err := takeValue()
			if err != nil {
				return out, err
			}
			out.output = v
		case "-include", "--include":
			v, err := takeValue()
			if err != nil {
				return out, err
			}
			out.includes = append(out.includes, v)
		case "-list", "--list":
			out.list = true
		case "-h", "-help", "--help":
			out.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return out, fmt.Errorf("unknown flag %s", arg)
			}
			if out.entry != "" {
				return out, fmt.Errorf("unexpected argument %s", arg)
			}
			out.entry = arg
		}
	}
	if !out.help && out.entry == "" {
		return out, errors.New("missing entry file")
	}
	return out, nil
}

// runBuild devolve o exit code: 0, 1 (falha do build), 2 (uso invalido).
func runBuild(args []string) int {
	parsed, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n%s", err, buildUsage)
		return 2
	}
	if parsed.help {
		fmt.Fprint(diagOut, buildUsage)
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: cannot locate the running noxy: %s\n", err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	opts := build.Options{Entry: parsed.entry, Output: parsed.output, Includes: parsed.includes, Runtime: exe, Out: os.Stdout, Diag: diagOut}
	plan, err := build.MakePlan(opts)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n", err)
		return 1
	}
	if parsed.list {
		if err := plan.List(os.Stdout); err != nil {
			fmt.Fprintf(diagOut, "noxy build: %s\n", err)
			return 1
		}
		return 0
	}
	out, size, err := build.Write(plan, opts)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n", err)
		return 1
	}
	names := make([]string, 0, len(plan.Extensions))
	for _, e := range plan.Extensions {
		names = append(names, e.Name)
	}
	extensions := fmt.Sprintf("%d extensions", len(names))
	if len(names) != 0 {
		extensions += " (" + strings.Join(names, ", ") + ")"
	}
	fmt.Fprintf(os.Stdout, "noxy build: %d modules, %s, %d included files\n", len(plan.Modules), extensions, len(plan.IncludedFiles()))
	fmt.Fprintf(os.Stdout, "noxy build: wrote %s (%s)\n", out, build.HumanSize(size))
	return 0
}
