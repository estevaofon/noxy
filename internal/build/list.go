package build

import (
	"fmt"
	"io"
	"os"
)

// List imprime o plano (o que --list mostra, spec §3.1).
func (p *Plan) List(w io.Writer) error {
	runtimeSize := "?"
	if info, err := os.Stat(p.Runtime); err == nil {
		runtimeSize = HumanSize(info.Size())
	}
	fmt.Fprintf(w, "entry      %s\n", p.Entry)
	fmt.Fprintf(w, "root       %s\n", p.Root)
	fmt.Fprintf(w, "target     %s\n", p.Target)
	fmt.Fprintf(w, "runtime    %s (%s)\n", p.Runtime, runtimeSize)
	fmt.Fprintf(w, "modules    %d\n", len(p.Modules))
	for _, m := range p.Modules {
		if m.Dir {
			fmt.Fprintf(w, "  %s (dir)\n", m.Path)
			continue
		}
		fmt.Fprintf(w, "  %s\n", m.Path)
	}
	fmt.Fprintf(w, "extensions %d\n", len(p.Extensions))
	for _, e := range p.Extensions {
		fmt.Fprintf(w, "  %s  %s  %s  %s/%s  %s\n", e.Name, e.Kind, p.Target, e.Dir, e.Artifact, HumanSize(e.Size))
	}
	included := p.IncludedFiles()
	var includedBytes int64
	for _, f := range included {
		includedBytes += f.Size
	}
	fmt.Fprintf(w, "includes   %d files, %s\n", len(included), HumanSize(includedBytes))
	for _, f := range included {
		fmt.Fprintf(w, "  %s  %s\n", f.Path, HumanSize(f.Size))
	}
	var total int64
	for _, f := range p.Files {
		total += f.Size
	}
	_, err := fmt.Fprintf(w, "payload    %d files, %s uncompressed\n", len(p.Files), HumanSize(total))
	return err
}

// HumanSize: "12 B", "1.5 KB", "22.7 MB" (base 1024, uma casa decimal).
func HumanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
