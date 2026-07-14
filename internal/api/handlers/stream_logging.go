package handlers

import (
	"regexp"

	"github.com/rs/zerolog/log"
)

var upstreamURLPattern = regexp.MustCompile(`(?i)https?://[^\s"']+`)

func logUpstreamError(message string, accountID uint, err error) {
	redacted := upstreamURLPattern.ReplaceAllString(err.Error(), "[redacted-url]")
	log.Error().Uint("accountID", accountID).Str("error", redacted).Msg(message)
}
