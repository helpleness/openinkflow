// Package fairness 提供与执行器无关的按用户轮转队列算法。
package fairness

import (
	"errors"
	"strings"
	"sync"
)

var ErrNilJob = errors.New("fair queue job is nil")

// Job 是公平队列中的最小任务单元。队列只决定取出顺序，不创建协程也不执行任务。
type Job struct {
	UserName string
	Run      func()
	// OnRejected 由取出任务的调度器在无法提交执行器时调用，例如 ants 池已经释放。
	// Queue 本身不会执行或拒绝任务，因此不会主动调用该回调。
	OnRejected func(error)
}

// Queue 维护每个用户独立的 FIFO 队列；每次 Dequeue 只取一个用户的一个任务，
// 再把仍有待办的用户放到队尾，从而防止单个用户长期挤占后续执行机会。
type Queue struct {
	mu           sync.Mutex
	queues       map[string][]Job
	readyUsers   []string
	inReady      map[string]bool
	pendingCount int
}

// NewQueue 创建一个独立的用户轮转队列，适合测试或需要独立执行域的场景。
func NewQueue() *Queue {
	return &Queue{
		queues:  make(map[string][]Job),
		inReady: make(map[string]bool),
	}
}

// Enqueue 将任务放入对应用户的队列。
func (queue *Queue) Enqueue(userName string, run func()) error {
	return queue.EnqueueJob(Job{UserName: userName, Run: run})
}

// EnqueueJob 将完整任务放入对应用户的队列。
// 它与 Enqueue 使用相同的轮转规则，额外允许调度器在任务无法提交执行器时通知调用方。
func (queue *Queue) EnqueueJob(job Job) error {
	if job.Run == nil {
		return ErrNilJob
	}
	job.UserName = normalizeUserName(job.UserName)
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.queues[job.UserName] = append(queue.queues[job.UserName], job)
	queue.pendingCount++
	if !queue.inReady[job.UserName] {
		queue.readyUsers = append(queue.readyUsers, job.UserName)
		queue.inReady[job.UserName] = true
	}
	return nil
}

// Dequeue 按用户轮转规则取出下一个任务。
func (queue *Queue) Dequeue() (Job, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for len(queue.readyUsers) > 0 {
		userName := queue.readyUsers[0]
		queue.readyUsers = queue.readyUsers[1:]
		jobs := queue.queues[userName]
		if len(jobs) == 0 {
			delete(queue.inReady, userName)
			continue
		}
		job := jobs[0]
		jobs = jobs[1:]
		queue.pendingCount--
		if len(jobs) == 0 {
			delete(queue.queues, userName)
			delete(queue.inReady, userName)
		} else {
			queue.queues[userName] = jobs
			queue.readyUsers = append(queue.readyUsers, userName)
		}
		return job, true
	}
	return Job{}, false
}

// Pending 返回尚未取出的任务总数。
func (queue *Queue) Pending() int {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return queue.pendingCount
}

func normalizeUserName(userName string) string {
	if normalized := strings.TrimSpace(userName); normalized != "" {
		return normalized
	}
	return "anonymous"
}
