package admin

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

// Core policy tests: port fakes only, no database (testing.md rule 3).

type fakeStatistics struct {
	bytesFailure error
}

func (statistics fakeStatistics) DatabaseBytes(context.Context) kit.Outcome[int64] {
	if statistics.bytesFailure != nil {
		return kit.Failure[int64](statistics.bytesFailure)
	}
	return kit.Success[int64](4096)
}

func (statistics fakeStatistics) Tables(context.Context) kit.Outcome[[]TableStat] {
	return kit.Success([]TableStat{{Name: "tenants", LiveRows: 2, TotalBytes: 8192}})
}

type fakeAccounts struct{}

func (fakeAccounts) Census(context.Context) kit.Outcome[identity.Census] {
	return kit.Success(identity.Census{Tenants: 2, Bindings: 3, PendingLinks: 1})
}

type fakeJobQueue struct {
	retried []int64
	deleted []int64
}

func (queue *fakeJobQueue) CountsByState(context.Context) kit.Outcome[map[string]int64] {
	return kit.Success(map[string]int64{"available": 1, "completed": 5})
}

func (queue *fakeJobQueue) TroubledJobs(context.Context, int) kit.Outcome[[]Job] {
	return kit.Success([]Job{{ID: 7, Kind: "registration_poll", State: "discarded"}})
}

func (queue *fakeJobQueue) Retry(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.retried = append(queue.retried, jobID)
	return kit.Done()
}

func (queue *fakeJobQueue) Delete(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.deleted = append(queue.deleted, jobID)
	return kit.Done()
}

func TestOverviewMergesAllFourSources(test *testing.T) {
	component := NewComponent(fakeStatistics{}, &fakeJobQueue{}, fakeAccounts{})

	overview := component.Overview(context.Background())

	if overview.DatabaseBytes != 4096 {
		test.Fatalf("database size missing: %+v", overview)
	}
	if overview.Accounts != (identity.Census{Tenants: 2, Bindings: 3, PendingLinks: 1}) {
		test.Fatalf("accounts census missing: %+v", overview.Accounts)
	}
	if len(overview.Tables) != 1 || overview.Tables[0].Name != "tenants" {
		test.Fatalf("table stats missing: %+v", overview.Tables)
	}
	if overview.JobCounts["completed"] != 5 {
		test.Fatalf("job counts missing: %+v", overview.JobCounts)
	}
	if len(overview.Partial) != 0 {
		test.Fatalf("nothing failed, yet Partial holds %v", overview.Partial)
	}
}

func TestOverviewDegradesPerSourceInsteadOfBlanking(test *testing.T) {
	// Given a statistics port whose size read fails
	boom := errors.New("catalog unreachable")
	component := NewComponent(fakeStatistics{bytesFailure: boom}, &fakeJobQueue{}, fakeAccounts{})

	overview := component.Overview(context.Background())

	// Then the failure lands in Partial and the other sources still render
	if len(overview.Partial) != 1 || !errors.Is(overview.Partial[0], boom) {
		test.Fatalf("expected the size failure in Partial, got %v", overview.Partial)
	}
	if len(overview.Tables) != 1 || overview.JobCounts["available"] != 1 {
		test.Fatalf("the healthy sources must survive: %+v", overview)
	}
}

func TestJobVerbsReachTheQueuePort(test *testing.T) {
	queue := &fakeJobQueue{}
	component := NewComponent(fakeStatistics{}, queue, fakeAccounts{})

	if outcome := component.RetryJob(context.Background(), 7); outcome.Failed() {
		test.Fatalf("retry failed: %v", outcome.Error())
	}
	if outcome := component.DeleteJob(context.Background(), 9); outcome.Failed() {
		test.Fatalf("delete failed: %v", outcome.Error())
	}
	if len(queue.retried) != 1 || queue.retried[0] != 7 {
		test.Fatalf("retry did not reach the port: %v", queue.retried)
	}
	if len(queue.deleted) != 1 || queue.deleted[0] != 9 {
		test.Fatalf("delete did not reach the port: %v", queue.deleted)
	}
}
