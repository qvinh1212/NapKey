package httpapi

import "time"

// timeIn returns a timestamp n hours from now.
func timeIn(hours int) time.Time {
	return time.Now().Add(time.Duration(hours) * time.Hour)
}
