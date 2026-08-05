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
	} else if capacityIsLow(snapshot.Accounts, snapshot.Available, out.AvailablePercent) {
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

// capacityIsLow reports whether the remaining capacity is worth warning about.
//
// "One left" only means nearly exhausted when there was more than one to begin with.
// The 9Router upstream is a single link, so it reports Accounts=1, Available=1 when
// perfectly healthy -- and a bare `Available <= 1` test called that degraded, which
// showed customers a permanent yellow status page for a system serving every request
// normally. Anything below one is already handled as an outage before this is reached.
//
// For a real pool the warning is unchanged: down to the last account, or a quarter of
// a pool of four or more still available.
func capacityIsLow(accounts, available int, availablePercent float64) bool {
	if accounts <= 1 {
		// Nothing to be low against: a single upstream is either serving or it is not.
		return false
	}
	return available <= 1 || (accounts >= 4 && availablePercent <= 25)
}

func roundPercent(value float64) float64 {
	return math.Round(value*10) / 10
}
