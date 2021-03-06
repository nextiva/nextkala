package job

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nextiva/nextkala/utils/iso8601"
)

type MockDBGetAll struct {
	MockDB
	response []*Job
}

func (d *MockDBGetAll) GetAll() ([]*Job, error) {
	return d.response, nil
}

type MockDB struct {
	Runs map[string]*JobStat
}

func (m *MockDB) GetAll() ([]*Job, error) {
	return nil, nil
}
func (m *MockDB) Get(id string) (*Job, error) {
	return nil, nil
}
func (m *MockDB) Delete(id string) error {
	return nil
}
func (m *MockDB) Save(job *Job) error {
	return nil
}
func (m *MockDB) Close() error {
	return nil
}

func (m *MockDB) SaveRun(jobStat *JobStat) error {
	m.Runs[jobStat.Id] = jobStat
	return nil
}

func (m *MockDB) UpdateRun(jobStat *JobStat) error {
	m.Runs[jobStat.Id] = jobStat
	return nil
}

func (m *MockDB) GetAllRuns(jobID string) ([]*JobStat, error) {
	stats := make([]*JobStat, 0)
	for _, value := range m.Runs {
		if value.JobId == jobID {
			stats = append(stats, value)
		}
	}
	return stats, nil
}

func (m *MockDB) GetRun(runID string) (*JobStat, error) {
	return m.Runs[runID], nil
}

func (m *MockDB) DeleteRun(jobId string) error {
	return nil
}

func (m *MockDB) ClearExpiredRuns() error {
	return nil
}

func NewMockCache() *LockFreeJobCache {
	db := &MockDB{Runs: make(map[string]*JobStat)}
	return NewLockFreeJobCache(db)
}

func GetMockJob() *Job {
	return &Job{
		Name:    "mock_job",
		Command: "bash -c 'date'",
		Owner:   "example@example.com",
		Retries: 2,
	}
}

func GetMockFailingJob() *Job {
	return &Job{
		Name:    "mock_failing_job",
		Command: "asdf",
		Owner:   "example@example.com",
		Retries: 2,
	}
}

func GetMockRemoteJob(props RemoteProperties) *Job {
	return &Job{
		Name:             "mock_remote_job",
		Command:          "",
		JobType:          RemoteJob,
		RemoteProperties: props,
	}
}

func GetMockJobWithSchedule(repeat int, scheduleTime time.Time, delay string) *Job {
	genericMockJob := GetMockJob()

	parsedTime := scheduleTime.Format(time.RFC3339)
	scheduleStr := fmt.Sprintf("R%d/%s/%s", repeat, parsedTime, delay)
	genericMockJob.Schedule = scheduleStr

	return genericMockJob
}

func GetMockRecurringJobWithSchedule(scheduleTime time.Time, delay string) *Job {
	genericMockJob := GetMockJob()

	parsedTime := scheduleTime.Format(time.RFC3339)
	scheduleStr := fmt.Sprintf("R/%s/%s", parsedTime, delay)
	genericMockJob.Schedule = scheduleStr
	genericMockJob.timesToRepeat = -1

	parsedDuration, _ := iso8601.FromString(delay)

	genericMockJob.delayDuration = parsedDuration

	return genericMockJob
}

func GetMockJobWithGenericSchedule(now time.Time) *Job {
	fiveMinutesFromNow := now.Add(time.Minute * 5)
	return GetMockJobWithSchedule(2, fiveMinutesFromNow, "P1DT10M10S")
}

func parseTime(t *testing.T, value string) time.Time {
	now, err := time.Parse("2006-Jan-02 15:04", value)
	if err != nil {
		t.Fatal(err)
	}
	return now
}

func parseTimeInLocation(t *testing.T, value string, location string) time.Time {
	loc, err := time.LoadLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.ParseInLocation("2006-Jan-02 15:04", value, loc)
	if err != nil {
		t.Fatal(err)
	}
	return now
}

// Used to hand off execution briefly so that jobs can run and so on.
func briefPause() {
	time.Sleep(time.Millisecond * 100)
}

func awaitJobRan(t *testing.T, j *Job, timeout time.Duration) {
	t.Helper()
	select {
	case <-j.ranChan:
	case <-time.After(timeout):
		t.Fatal("Job failed to run")
	}
}

var _ JobDB = (*MemoryDB)(nil)

type MemoryDB struct {
	m    map[string]*Job
	runs map[string][]*JobStat
	lock sync.RWMutex
}

func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		m:    map[string]*Job{},
		runs: map[string][]*JobStat{},
	}
}

func (m *MemoryDB) GetAll() (ret []*Job, _ error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for _, v := range m.m {
		ret = append(ret, v)
	}
	return
}

func (m *MemoryDB) Get(id string) (*Job, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	j, exist := m.m[id]
	if !exist {
		return nil, ErrJobNotFound(id)
	}
	return j, nil
}

func (m *MemoryDB) Delete(id string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	if _, exists := m.m[id]; !exists {
		return errors.New("Doesn't exist") // Used for testing
	}
	delete(m.m, id)
	// log.Printf("After delete: %+v", m)
	return nil
}

func (m *MemoryDB) Save(j *Job) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.m[j.Id] = j
	return nil
}

func (m *MemoryDB) SaveRun(run *JobStat) error {
	m.runs[run.JobId] = append(m.runs[run.JobId], run)
	return nil
}

func (m *MemoryDB) UpdateRun(jobStat *JobStat) error {
	runs := m.runs[jobStat.JobId]
	for i, run := range runs {
		if run.Id == jobStat.Id {
			runs[i] = jobStat
		}
	}
	return m.SaveRun(jobStat)
}

func (m *MemoryDB) GetAllRuns(jobID string) (ret []*JobStat, _ error) {
	for ID, runs := range m.runs {
		for _, run := range runs {
			if ID == jobID {
				ret = append(ret, run)
			}
		}
	}
	return
}

func (m *MemoryDB) GetRun(runID string) (ret *JobStat, _ error) {
	for _, runs := range m.runs {
		for _, run := range runs {
			if run.Id == runID {
				return run, nil
			}
		}
	}
	return
}

func (m *MemoryDB) DeleteRun(id string) error {
	return nil
}

func (m *MemoryDB) ClearExpiredRuns() error {
	return nil
}

func (m *MemoryDB) Close() error {
	return nil
}
