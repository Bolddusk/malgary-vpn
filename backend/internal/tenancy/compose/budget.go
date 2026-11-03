package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

var memLimitRE = regexp.MustCompile(`(?m)^\s+mem_limit:\s*(\d+)m\s*$`)

// MemoryOf sums the mem_limit values (MB) in a rendered compose file.
func MemoryOf(compose []byte) int {
	total := 0
	for _, m := range memLimitRE.FindAllSubmatch(compose, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		total += n
	}
	return total
}

// checkBudget refuses a deploy that would take all Malgary containers over the memory budget
// (FR-005, SC-004): the platform's fixed share plus every stack's limits, with this tenant's
// stack counted at its new size.
func (d *Deployer) checkBudget(slug string, compose []byte) error {
	if d.BudgetMB == 0 {
		return nil
	}
	total := d.PlatformMB + MemoryOf(compose)
	others, _ := filepath.Glob(filepath.Join(d.StackDir, "*", "compose.yaml"))
	for _, p := range others {
		if filepath.Base(filepath.Dir(p)) == slug {
			continue
		}
		b, err := os.ReadFile(p) //nolint:gosec // files under the stack directory
		if err != nil {
			return err
		}
		total += MemoryOf(b)
	}
	if total > d.BudgetMB {
		return fmt.Errorf("memory budget: stack %s would bring Malgary to %d MB of %d MB allowed", slug, total, d.BudgetMB)
	}
	return nil
}
