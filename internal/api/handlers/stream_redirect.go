package handlers

import (
	"net/url"
	"strings"
)

func validRedirectURL(value string) bool {
	redirectURL, err := url.Parse(value)
	return err == nil && redirectURL.User == nil && redirectURL.Host != "" && (redirectURL.Scheme == "http" || redirectURL.Scheme == "https")
}

func normalizeRedirectURL(value, trustedBase string) (string, bool) {
	redirectURL, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	if redirectURL.IsAbs() {
		if !validRedirectURL(redirectURL.String()) {
			return "", false
		}
		return redirectURL.String(), true
	}
	if trustedBase == "" || redirectURL.Host != "" || strings.HasPrefix(value, "//") {
		return "", false
	}
	baseURL, err := url.Parse(trustedBase)
	if err != nil || !validRedirectURL(baseURL.String()) {
		return "", false
	}
	resolveBase := *baseURL
	if !strings.HasPrefix(redirectURL.Path, "/") {
		resolveBase.Path = strings.TrimRight(resolveBase.Path, "/") + "/"
		resolveBase.RawPath = ""
	}
	resolved := resolveBase.ResolveReference(redirectURL)
	if resolved.Scheme != baseURL.Scheme || resolved.Host != baseURL.Host || !validRedirectURL(resolved.String()) {
		return "", false
	}
	return resolved.String(), true
}
