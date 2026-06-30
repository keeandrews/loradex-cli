package cmd

import "time"

func clampLimit(n int) int {
	switch {
	case n < 1:
		return 1
	case n > 100:
		return 100
	default:
		return n
	}
}

func clampPage(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func short16(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// shortDate renders an ISO-8601 timestamp as YYYY-MM-DD (best effort).
func shortDate(iso string) string {
	if iso == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, iso); err == nil {
		return t.Format("2006-01-02")
	}
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}
