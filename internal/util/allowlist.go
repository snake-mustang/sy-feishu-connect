package util

import "strings"

func Allowed(list, value string) bool {
	list = strings.TrimSpace(list)
	if list == "" || list == "*" {
		return true
	}
	value = strings.TrimSpace(value)
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == value {
			return true
		}
	}
	return false
}
