package rule

import (
	"errors"
	"fmt"
	"testing"
)

func TestRecordRequestStats(t *testing.T) {
	f := &HTTPFetcher{sourceID: "stats-test"}
	f.recordRequest(nil)
	f.recordRequest(ErrChallenge)
	f.recordRequest(fmt.Errorf("HTTP 403 denied"))
	f.recordRequest(errors.New("boom"))

	snap := StatsSnapshot()["stats-test"]
	if snap.OK != 1 || snap.Challenge != 1 || snap.Blocked != 1 || snap.Failed != 1 {
		t.Fatalf("统计不对: %+v", snap)
	}
}
