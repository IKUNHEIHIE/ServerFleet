package tasks

type taskChange struct {
	ready chan struct{}
	users int
}

func (s *Service) wakeScheduler() {
	select {
	case s.schedulerWake <- struct{}{}:
	default:
	}
}

// WatchTaskChange subscribes to the next committed decision or terminal update.
// Callers subscribe before reading, cancel when done, and read through their
// normal authorization gate after a wake. The channel carries no task data.
func (s *Service) WatchTaskChange(taskID string) (<-chan struct{}, func()) {
	s.changeMu.Lock()
	change := s.taskChanges[taskID]
	if change == nil {
		change = &taskChange{ready: make(chan struct{})}
		s.taskChanges[taskID] = change
	}
	change.users++
	s.changeMu.Unlock()
	return change.ready, func() {
		s.changeMu.Lock()
		defer s.changeMu.Unlock()
		if s.taskChanges[taskID] == change {
			change.users--
			if change.users == 0 {
				delete(s.taskChanges, taskID)
			}
		}
	}
}

func (s *Service) notifyTaskChanged(taskID string) {
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	if change := s.taskChanges[taskID]; change != nil {
		delete(s.taskChanges, taskID)
		close(change.ready)
	}
}
