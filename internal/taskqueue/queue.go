package taskqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
)

type Task struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	ProjectID string         `json:"project_id"`
	Payload   map[string]any `json:"payload"`
}

type Record struct {
	Task
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type Handler func(context.Context, Task) error

type Queue struct {
	client      *asynq.Client
	inspector   *asynq.Inspector
	server      *asynq.Server
	queueName   string
	concurrency int
}

func NewRedis(opt asynq.RedisClientOpt, queueName string, concurrency int, handlers map[string]Handler) *Queue {
	if queueName == "" {
		queueName = "packaging-ai"
	}
	if concurrency < 1 {
		concurrency = 1
	}
	client := asynq.NewClient(opt)
	inspector := asynq.NewInspector(opt)
	server := asynq.NewServer(opt, asynq.Config{Concurrency: concurrency, Queues: map[string]int{queueName: 1}})
	mux := asynq.NewServeMux()
	for taskType, handler := range handlers {
		taskType, handler := taskType, handler
		mux.HandleFunc(taskType, func(ctx context.Context, task *asynq.Task) error {
			var input Task
			if err := json.Unmarshal(task.Payload(), &input); err != nil {
				return fmt.Errorf("decode task: %w", err)
			}
			return handler(ctx, input)
		})
	}
	go func() { _ = server.Run(mux) }()
	return &Queue{client: client, inspector: inspector, server: server, queueName: queueName, concurrency: concurrency}
}

func (q *Queue) Submit(ctx context.Context, task Task) error {
	if task.ID == "" || task.Kind == "" {
		return errors.New("task id and kind are required")
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(task.Kind, payload), asynq.Queue(q.queueName), asynq.TaskID(task.ID), asynq.MaxRetry(3), asynq.Retention(24*time.Hour))
	return err
}

func (q *Queue) Get(id string) (Record, bool) {
	info, err := q.inspector.GetTaskInfo(q.queueName, id)
	if err != nil || info == nil {
		return Record{}, false
	}
	status := mapStatus(info.State.String())
	var task Task
	if err := json.Unmarshal(info.Payload, &task); err != nil {
		task = Task{ID: info.ID, Kind: info.Type}
	}
	return Record{Task: task, Status: status, Error: info.LastErr, CreatedAt: info.NextProcessAt, UpdatedAt: info.CompletedAt}, true
}

func (q *Queue) Close() {
	q.server.Shutdown()
	_ = q.client.Close()
	_ = q.inspector.Close()
}

func mapStatus(state string) Status {
	switch state {
	case "pending", "scheduled", "retry", "aggregating":
		return Queued
	case "active":
		return Running
	case "completed":
		return Succeeded
	case "archived", "failed":
		return Failed
	default:
		return Queued
	}
}
