package sysinfo

import (
	"testing"
)

// TestParseDistroFromOSRelease verifies parsing of /etc/os-release content.
func TestParseDistroFromOSRelease(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "normal quoted PRETTY_NAME",
			content:  `PRETTY_NAME="Ubuntu 24.04.1 LTS"`,
			expected: "Ubuntu 24.04.1 LTS",
		},
		{
			name:     "unquoted PRETTY_NAME",
			content:  `PRETTY_NAME=Arch Linux`,
			expected: "Arch Linux",
		},
		{
			name:     "multiple fields with PRETTY_NAME",
			content:  "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"",
			expected: "Ubuntu 24.04.1 LTS",
		},
		{
			name:     "missing PRETTY_NAME",
			content:  "NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"",
			expected: "unknown",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "unknown",
		},
		{
			name:     "PRETTY_NAME with quotes and spaces",
			content:  `PRETTY_NAME="Debian GNU/Linux 12.5"`,
			expected: "Debian GNU/Linux 12.5",
		},
		{
			name:     "PRETTY_NAME first in multiline",
			content:  "PRETTY_NAME=\"Fedora 39\"\nNAME=\"Fedora\"\nVERSION_ID=\"39\"",
			expected: "Fedora 39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDistroFromOSRelease(tt.content)
			if result != tt.expected {
				t.Errorf("parseDistroFromOSRelease(%q) = %q, expected %q", tt.content, result, tt.expected)
			}
		})
	}
}

// TestParseDistroFromSwVers verifies parsing of sw_vers output.
func TestParseDistroFromSwVers(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "normal sw_vers output",
			content:  "ProductName:\tmacOS\nProductVersion:\t15.2",
			expected: "macOS 15.2",
		},
		{
			name:     "space-separated format",
			content:  "ProductName: macOS\nProductVersion: 15.2",
			expected: "macOS 15.2",
		},
		{
			name:     "missing ProductVersion",
			content:  "ProductName:\tmacOS",
			expected: "unknown",
		},
		{
			name:     "missing ProductName",
			content:  "ProductVersion:\t15.2",
			expected: "unknown",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "unknown",
		},
		{
			name:     "multiline with BuildVersion",
			content:  "ProductName:\tmacOS\nProductVersion:\t15.2\nBuildVersion:\t24C101",
			expected: "macOS 15.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDistroFromSwVers(tt.content)
			if result != tt.expected {
				t.Errorf("parseDistroFromSwVers(%q) = %q, expected %q", tt.content, result, tt.expected)
			}
		})
	}
}
