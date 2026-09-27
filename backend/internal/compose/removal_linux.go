//go:build linux

package compose

import (
	"errors"
	"os"
	"strings"
)

func checkRemovalMounts(path string) error {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return errors.New("无法检查挂载边界，禁止整体删除目录")
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if PathContains(path, unescape.Replace(fields[4])) {
			return errors.New("目录包含挂载点，禁止整体删除")
		}
	}
	return nil
}
