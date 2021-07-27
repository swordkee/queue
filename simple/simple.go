package simple

import (
	"context"
	"errors"
	"sync"

	"github.com/appleboy/queue"
)

const defaultQueueSize = 4096

var _ queue.Worker = (*Worker)(nil)

// Option for queue system
type Option func(*Worker)

var errMaxCapacity = errors.New("max capacity reached")

// Worker for simple queue using channel
type Worker struct {
	taskQueue chan queue.QueuedMessage
	runFunc   func(context.Context, queue.QueuedMessage) error
	stop      chan struct{}
	logger    queue.Logger
	stopOnce  sync.Once
}

// BeforeRun run script before start worker
func (s *Worker) BeforeRun() error {
	return nil
}

// AfterRun run script after start worker
func (s *Worker) AfterRun() error {
	return nil
}

func (s *Worker) handle(m interface{}) error {
	// create channel with buffer size 1 to avoid goroutine leak
	done := make(chan error, 1)
	panicChan := make(chan interface{}, 1)
	job, _ := m.(queue.Job)
	ctx, cancel := context.WithTimeout(context.Background(), job.Timeout)
	defer cancel()

	// run the job
	go func() {
		// handle panic issue
		defer func() {
			if p := recover(); p != nil {
				panicChan <- p
			}
		}()

		// run custom process function
		done <- s.runFunc(ctx, job)
	}()

	select {
	case p := <-panicChan:
		panic(p)
	case <-ctx.Done(): // timeout reached
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			s.logger.Infof("job timeout: %s", job.Timeout.String())
		}
		// wait job
		return <-done
	case <-s.stop: // shutdown service
		cancel()
		// wait job
		return <-done
	case err := <-done: // job finish and continue to worker
		return err
	}
}

// Run start the worker
func (s *Worker) Run() error {
	// check queue status
	select {
	case <-s.stop:
		return queue.ErrQueueShutdown
	default:
	}

	for task := range s.taskQueue {
		if err := s.handle(task); err != nil {
			s.logger.Error(err.Error())
		}
	}
	return nil
}

// Shutdown worker
func (s *Worker) Shutdown() error {
	s.stopOnce.Do(func() {
		close(s.stop)
		close(s.taskQueue)
	})
	return nil
}

// Capacity for channel
func (s *Worker) Capacity() int {
	return cap(s.taskQueue)
}

// Usage for count of channel usage
func (s *Worker) Usage() int {
	return len(s.taskQueue)
}

// Queue send notification to queue
func (s *Worker) Queue(job queue.QueuedMessage) error {
	select {
	case <-s.stop:
		return queue.ErrQueueShutdown
	default:
	}

	select {
	case s.taskQueue <- job:
		return nil
	default:
		return errMaxCapacity
	}
}

// WithQueueNum setup the capcity of queue
func WithQueueNum(num int) Option {
	return func(w *Worker) {
		w.taskQueue = make(chan queue.QueuedMessage, num)
	}
}

// WithRunFunc setup the run func of queue
func WithRunFunc(fn func(context.Context, queue.QueuedMessage) error) Option {
	return func(w *Worker) {
		w.runFunc = fn
	}
}

// WithLogger set custom logger
func WithLogger(l queue.Logger) Option {
	return func(w *Worker) {
		w.logger = l
	}
}

// NewWorker for struc
func NewWorker(opts ...Option) *Worker {
	w := &Worker{
		taskQueue: make(chan queue.QueuedMessage, defaultQueueSize),
		stop:      make(chan struct{}),
		logger:    queue.NewLogger(),
		runFunc: func(context.Context, queue.QueuedMessage) error {
			return nil
		},
	}

	// Loop through each option
	for _, opt := range opts {
		// Call the option giving the instantiated
		opt(w)
	}

	return w
}
