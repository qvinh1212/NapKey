package reliability

import "math"

type Status string

const (
	StatusOperational Status = "operational"
	StatusDegraded    Status = "degraded"
	StatusOutage      Status = "outage"
)

type DataPlaneSnapshot struct {
	Accounts       int
	Available      int
	RecentRequests int64
	RecentFailures int64
	UsageHealthy   int64
	UsagePending   int64
	UsageDropped   int64
}

type Issue struct {
	Code     string `json:"code"`
	Severity Status `json:"severity"`
}

type Assessment struct {
	Status           Status  `json:"status"`
	Issues           []Issue `json:"issues"`
	ErrorRatePercent float64 `json:"errorRatePercent"`
	AvailablePercent float64 `json:"availablePercent"`
}

func Evaluate(postgresOK bool, snapshot *DataPlaneSnapshot, dataPlaneErr error) Assessment {
	out := Assessment{Status: StatusOperational, Issues: []Issue{}}
	add := func(code string, severity Status) {
		out.Issues = append(out.Issues, Issue{Code: code, Severity: severity})
		if severity == StatusOutage || (severity == StatusDegraded && out.Status == StatusOperational) {
			out.Status = severity
		}
	}

	if !postgresOK {
		add("postgres_unreachable", StatusOutage)
	}
	if dataPlaneErr != nil || snapshot == nil {
		add("data_plane_unreachable", StatusOutage)
		return out
	}

	if snapshot.Accounts > 0 {
		out.AvailablePercent = roundPercent(float64(snapshot.Available) / float64(snapshot.Accounts) * 100)
	}
	if snapshot.Available <= 0 {
		add("upstream_capacity_empty", StatusOutage)
	} else if snapshot.Available <= 1 || (snapshot.Accounts >= 4 && out.AvailablePercent <= 25) {
		add("upstream_capacity_low", StatusDegraded)
	}
	if snapshot.UsageHealthy != 1 {
		add("usage_reporting_unhealthy", StatusOutage)
	}
	if snapshot.UsageDropped > 0 {
		add("usage_reports_dropped", StatusOutage)
	}
	if snapshot.UsagePending >= 20 {
		add("usage_backlog_high", StatusDegraded)
	}
	if snapshot.RecentRequests > 0 {
		out.ErrorRatePercent = roundPercent(float64(snapshot.RecentFailures) / float64(snapshot.RecentRequests) * 100)
	}
	if snapshot.RecentRequests >= 20 && out.ErrorRatePercent >= 10 {
		add("error_rate_high", StatusDegraded)
	}
	return out
}

func roundPercent(value float64) float64 {
	return math.Round(value*10) / 10
}
