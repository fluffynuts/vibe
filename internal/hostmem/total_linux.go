package hostmem

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// total reads MemTotal from /proc/meminfo, which is in KiB.
func total() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kib * 1024, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("no MemTotal in /proc/meminfo")
}
