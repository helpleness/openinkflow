package fairness

import "testing"

func TestQueueRoundRobinsUsers(t *testing.T) {
	queue := NewQueue()
	for _, userName := range []string{"alice", "alice", "bob"} {
		if err := queue.Enqueue(userName, func() {}); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []string{"alice", "bob", "alice"} {
		job, ok := queue.Dequeue()
		if !ok || job.UserName != want {
			t.Fatalf("dequeue = %#v, %v; want user %q", job, ok, want)
		}
	}
	if _, ok := queue.Dequeue(); ok || queue.Pending() != 0 {
		t.Fatal("queue should be empty")
	}
}
