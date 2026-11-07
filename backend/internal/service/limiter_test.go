package service

import (
	"sync"
	"testing"
	"time"
)

func newLimiter(max int) *Service {
	s := &Service{limMax: max}
	s.limCond = sync.NewCond(&s.limMu)
	return s
}

func TestLimiterCapsConcurrency(t *testing.T) {
	s := newLimiter(2)
	if !s.tryAcquireSlot() || !s.tryAcquireSlot() {
		t.Fatal("前两个名额应当拿到")
	}
	if s.tryAcquireSlot() {
		t.Fatal("超出上限仍然拿到名额")
	}
	s.releaseSlot()
	if !s.tryAcquireSlot() {
		t.Fatal("释放后应当能再拿到名额")
	}
}

func TestLimiterBlocksThenWakes(t *testing.T) {
	s := newLimiter(1)
	if !s.tryAcquireSlot() {
		t.Fatal("首个名额应当拿到")
	}

	done := make(chan struct{})
	go func() {
		s.acquireSlot()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("名额占满时不应立即返回")
	case <-time.After(120 * time.Millisecond):
	}

	s.releaseSlot()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("释放名额后等待方没有被唤醒")
	}
}

// 调大上限要能立刻唤醒排队中的任务，否则改设置必须重启才生效
func TestLimiterRaisingMaxWakesWaiters(t *testing.T) {
	s := newLimiter(1)
	s.tryAcquireSlot()

	done := make(chan struct{})
	go func() {
		s.acquireSlot()
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)

	s.SetMaxConcurrentScans(2)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("调大上限后等待方没有被唤醒")
	}
}

func TestLimiterClampsToAtLeastOne(t *testing.T) {
	s := newLimiter(1)
	s.SetMaxConcurrentScans(0)
	if !s.tryAcquireSlot() {
		t.Fatal("上限被设为 0 时应回落到 1")
	}
	if s.tryAcquireSlot() {
		t.Fatal("回落后仍只应有 1 个名额")
	}
}
