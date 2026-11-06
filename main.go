// Gloss is a light desktop app that translates and explains selected text.
// Everything happens in core and app; start reading at docs/ARCHITECTURE.md.
//
// With no arguments it runs the app. The subcommands read what memory kept
// on this machine, for the local trial:
//
//	gloss stats [days]          what routing did, and how much is unknown
//	gloss recent [n]            the latest sessions, without their text
//	gloss label <session> right|wrong
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gloss/app"
	"gloss/memory"
	"gloss/settings"
)

func main() {
	if len(os.Args) > 1 {
		if err := command(os.Args[1], os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func command(name string, args []string) error {
	path, err := settings.Path(app.Name)
	if err != nil {
		return err
	}
	// Read as the file is, never trimmed or rewritten here: the app may be
	// running, and only it knows the retention the config asks for.
	dir := filepath.Join(filepath.Dir(path), "memory")
	file := filepath.Join(dir, "usage.jsonl")
	records := func() ([]memory.Record, error) { return memory.Read(dir) }
	switch name {
	case "stats":
		days := 7
		if len(args) > 0 {
			if days, err = strconv.Atoi(args[0]); err != nil {
				return fmt.Errorf("天数应为整数")
			}
		}
		rs, err := records()
		if err != nil {
			return err
		}
		fmt.Printf("最近 %d 天（%s）\n%s\n", days, file, memory.Summarize(rs, time.Now().AddDate(0, 0, -days)))
	case "recent":
		n := 20
		if len(args) > 0 {
			if n, err = strconv.Atoi(args[0]); err != nil {
				return fmt.Errorf("条数应为整数")
			}
		}
		rs, err := records()
		if err != nil {
			return err
		}
		for _, r := range rs[max(0, len(rs)-n):] {
			label := ""
			if r.Confirmed != nil {
				label = map[bool]string{true: " ✓", false: " ✗"}[*r.Confirmed]
			}
			fmt.Printf("%s  %s  %-28s %-9s %s→%s (%s) 改正 %d 次 %s%s\n", r.Session, r.Time.Local().Format("01-02 15:04"),
				r.App, r.Shape.Class(), r.Initial, r.Final, r.InitialBy, len(r.Corrections), r.Outcome, label)
		}
	case "label":
		if len(args) != 2 || args[1] != "right" && args[1] != "wrong" {
			return fmt.Errorf("用法：gloss label <session> right|wrong")
		}
		return memory.Label(dir, args[0], args[1] == "right")
	default:
		return fmt.Errorf("未知命令 %s：可用 stats、recent、label", name)
	}
	return nil
}
