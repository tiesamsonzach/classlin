package api

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrBadChannel   = errors.New("bad channel")
	ErrBadToken     = errors.New("bad token")
)

func ValidChannel(s string) bool {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 32 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
		switch r {
		case '/', '\\', '?', '#', '&', '%', '"', '\'', '<', '>', ';':
			return false
		}
	}
	return true
}

func ValidToken(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func CheckConfiguredToken(token string) error {
	if token == "" {
		return nil
	}
	if !ValidToken(token) {
		return errors.New("TOKEN 只能使用英文字母、数字和符号，不能有空格，最长 64 位")
	}
	return nil
}
