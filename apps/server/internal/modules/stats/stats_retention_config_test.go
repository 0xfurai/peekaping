package stats

import "testing"

func TestStatsRetentionCoversEverySummaryWindow(t *testing.T) {
	for _, summary := range summaryWindows {
		t.Run(string(summary.Period), func(t *testing.T) {
			for _, policy := range retention {
				if policy.Period == summary.Period {
					if policy.Keep < summary.Window {
						t.Errorf("%s stats are kept for %s but summaries read %s", summary.Period, policy.Keep, summary.Window)
					}
					return
				}
			}
			// A period without a retention policy is kept indefinitely.
		})
	}
}
