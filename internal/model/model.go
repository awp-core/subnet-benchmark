package model

import "time"

// Question status constants
const (
	QuestionStatusSubmitted = "submitted"
	QuestionStatusScored    = "scored"
)

// Assignment status constants
const (
	AssignmentStatusClaimed  = "claimed"
	AssignmentStatusReplied  = "replied"
	AssignmentStatusScored   = "scored"
	AssignmentStatusTimedOut = "timed-out"
)

// BenchmarkSet status constants
const (
	BSStatusActive   = "active"
	BSStatusInactive = "inactive"
)

type Worker struct {
	Address        string
	SuspendedUntil *time.Time
	EpochViolations int
	LastPollAt     *time.Time
	CreatedAt      time.Time
}

// IsSuspended returns true if the worker is currently suspended.
func (w *Worker) IsSuspended() bool {
	return w.SuspendedUntil != nil && w.SuspendedUntil.After(time.Now())
}

type BenchmarkSet struct {
	SetID                string `json:"set_id"`
	Description          string `json:"description"`
	QuestionRequirements string `json:"question_requirements"`
	AnswerRequirements   string `json:"answer_requirements"`
	QuestionMaxLen       int    `json:"question_maxlen"`
	AnswerMaxLen         int    `json:"answer_maxlen"`
	AnswerCheckMethod    string `json:"answer_check_method"`
	Status               string `json:"status"`
	TotalQuestions       int    `json:"total_questions"`
	QualifiedQuestions   int    `json:"qualified_questions"`
	CreatedAt            time.Time
}

type Question struct {
	QuestionID int64
	BSID       string
	Questioner string
	Question   string
	Answer     string
	Status     string
	Share      float64
	Score      int
	PassRate   float64
	Benchmark  bool
	MinHash    []byte
	CreatedAt  time.Time
	ScoredAt   *time.Time
}

type Assignment struct {
	AssignmentID int64
	QuestionID   int64
	Worker       string
	Status       string
	ReplyDDL     time.Time
	ReplyValid   *bool
	ReplyAnswer  *string
	RepliedAt    *time.Time
	Share        float64
	Score        int
	CreatedAt    time.Time
}

type Epoch struct {
	EpochDate   time.Time
	TotalReward int64
	TotalScored int
	SettledAt   *time.Time
	MerkleRoot  *string
	PublishedAt *time.Time
}

type WorkerEpochReward struct {
	EpochDate       time.Time
	WorkerAddress    string
	Recipient       *string
	ScoredAsks      int
	ScoredAnswers   int
	TimedOutAnswers int
	AskScoreSum     int
	AnswerScoreSum  int
	RawReward       int64
	CompositeScore  float64
	FinalReward     int64
}
