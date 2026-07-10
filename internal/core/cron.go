package core

import (
	"fmt"

	"github.com/robfig/cron/v3"
)

var (
	standardCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	secondsCronParser  = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
)

// ParseCronSpec validates a cron expression and reports whether it needs the
// scheduler's seconds-aware parser.
func ParseCronSpec(spec string) (bool, error) {
	if _, err := standardCronParser.Parse(spec); err == nil {
		return false, nil
	}
	if _, err := secondsCronParser.Parse(spec); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("Cron 表达式格式错误")
}
