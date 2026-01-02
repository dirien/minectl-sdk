// Package common provides shared utilities and constants for the minectl SDK.
package common //nolint:revive // package name is acceptable for SDK shared utilities

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// InstanceTag is the tag used to identify minectl instances.
const InstanceTag = "minectl"

// NameRegex is the regex pattern for valid server names.
const NameRegex = "^[a-z-0-9]+$"

// Green returns a green-colored string for terminal output.
func Green(value string) string {
	return color.GreenString(value)
}

// CreateServerNameWithTags creates a server name with tags.
func CreateServerNameWithTags(instanceName, label string) (id string) {
	return fmt.Sprintf("%s|%s", instanceName, label)
}

// StringPtr returns a pointer to the given string.
func StringPtr(s string) *string {
	return &s
}

// ExtractFieldsFromServername extracts labels from a server name ID.
func ExtractFieldsFromServername(id string) (label string, err error) {
	fields := strings.Split(id, "|")
	if len(fields) == 3 {
		label = strings.Join([]string{fields[1], fields[2]}, ",")
	} else {
		err = fmt.Errorf("could not get fields from custom ID: fields: %v", fields)
		return "", err
	}
	return label, nil
}
