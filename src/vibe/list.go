package main

import (
	"fmt"
	"sort"

	"vibe/internal/sbxrun"
)

func doList() error {
	statuses, err := sbxrun.List()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		note("no sandboxes found")
		return nil
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	for _, s := range statuses {
		text := "not running"
		if s.Running {
			text = "running"
		}
		fmt.Printf("%-16s%s\n", s.Name, text)
	}
	return nil
}
