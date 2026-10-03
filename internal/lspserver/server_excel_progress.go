package lspserver

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/excel"
)

type serverProgressTask struct {
	ID                 string
	Kind               string
	Label              string
	Detail             string
	DocumentURI        string
	DocumentVersion    int
	HasDocumentVersion bool
	Current            int
	Total              int
	ActiveItems        []string
	Cancellable        bool
	State              string
	StartedAt          int64
	UpdatedAt          int64
}

type progressStatusPublication struct {
	status map[string]any
	done   chan struct{}
	// superseded holds the done channels of replaced pending publications.
	// This snapshot was taken later and includes their state, so their waiters
	// are released only once it is written.
	superseded []chan struct{}
}

func (publication *progressStatusPublication) complete() {
	close(publication.done)
	for _, done := range publication.superseded {
		close(done)
	}
}

func progressDetailForURI(uri string) string {
	if path := fileURIPath(uri); path != "" {
		return filepath.Base(path)
	}
	return uri
}

func (s *Server) beginProgressTask(prefix, kind, reason, label, detail string, total int, cancellable bool) (string, int64) {
	return s.beginProgressTaskWithDocument(prefix, kind, reason, label, detail, "", 0, false, total, cancellable, false)
}

func (s *Server) beginDocumentProgressTask(prefix, kind, reason, label, detail, documentURI string, documentVersion int, hasDocumentVersion bool, total int, cancellable bool) (string, int64) {
	return s.beginProgressTaskWithDocument(prefix, kind, reason, label, detail, documentURI, documentVersion, hasDocumentVersion, total, cancellable, true)
}

func (s *Server) beginProgressTaskWithDocument(prefix, kind, reason, label, detail, documentURI string, documentVersion int, hasDocumentVersion bool, total int, cancellable, waitForPublication bool) (string, int64) {
	startedAt := time.Now().UnixMilli()
	taskID := s.nextProgressTaskID(prefix)
	s.mu.Lock()
	s.progressTasks[taskID] = &serverProgressTask{
		ID: taskID, Kind: kind, Label: label, Detail: detail,
		DocumentURI: documentURI, DocumentVersion: documentVersion, HasDocumentVersion: hasDocumentVersion,
		Total:       max(total, 0),
		Cancellable: cancellable, State: "running", StartedAt: startedAt, UpdatedAt: startedAt,
	}
	s.mu.Unlock()
	done := s.publishProgressTasksImmediate(reason)
	if waitForPublication && done != nil {
		<-done
	}
	return taskID, startedAt
}

func (s *Server) updateProgressTask(taskID, reason, label, detail string, current, total int, activeItems []string, state string) {
	s.updateProgressTaskWithPublication(taskID, reason, label, detail, current, total, activeItems, state, false)
}

func (s *Server) updateProgressTaskImmediate(taskID, reason, label, detail string, current, total int, activeItems []string, state string) <-chan struct{} {
	return s.updateProgressTaskWithPublication(taskID, reason, label, detail, current, total, activeItems, state, true)
}

func (s *Server) updateProgressTaskWithPublication(taskID, reason, label, detail string, current, total int, activeItems []string, state string, immediate bool) <-chan struct{} {
	s.mu.Lock()
	task := s.progressTasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return nil
	}
	previousLabel := task.Label
	previousTotal := task.Total
	if label != "" {
		task.Label = label
	}
	task.Detail = detail
	if current < 0 {
		current = 0
	}
	if total < 0 {
		total = 0
	}
	if total > 0 && current > total {
		current = total
	}
	if previousLabel == task.Label && previousTotal == total && current < task.Current {
		current = task.Current
	}
	task.Current = current
	task.Total = total
	task.ActiveItems = append(task.ActiveItems[:0], activeItems...)
	if state != "" {
		task.State = state
	}
	task.UpdatedAt = time.Now().UnixMilli()
	phaseChanged := previousLabel != task.Label
	s.mu.Unlock()
	if immediate || phaseChanged {
		done := s.publishProgressTasksImmediate(reason)
		if phaseChanged && done != nil {
			<-done
		}
		return done
	}
	s.publishProgressTasks(reason)
	return nil
}

func (s *Server) finishProgressTask(taskID, reason, state string) {
	s.mu.Lock()
	task := s.progressTasks[taskID]
	if task == nil {
		s.mu.Unlock()
		return
	}
	if state == "" {
		state = task.State
		if state == "running" {
			state = "completed"
		}
	}
	delete(s.progressTasks, taskID)
	s.mu.Unlock()
	done := s.publishProgressTasksImmediate(reason)
	if done != nil {
		<-done
	}
}

func (s *Server) beginAnalysisExcelProgressTask(targetPath string) (string, int64) {
	return s.beginProgressTask("excel.export", "analyzing", "excel.export", "excel.graph", targetPath, 0, true)
}

func (s *Server) analysisExcelProgressReporter(targetPath, taskID string, _ int64) func(excel.AnalysisProgressEvent) {
	return func(event excel.AnalysisProgressEvent) {
		detail := event.Detail
		if detail == "" {
			detail = targetPath
		}
		s.updateProgressTask(taskID, "excel.export", event.Label, detail, event.Current, event.Total, event.ActiveItems, "running")
	}
}

func analysisExcelGraphProgressReporter(progress func(excel.AnalysisProgressEvent)) graphProgressReporter {
	if progress == nil {
		return nil
	}
	return func(label, detail string, current, total int) {
		if label == "" {
			label = "graph"
		}
		activeItems := []string(nil)
		if detail != "" {
			activeItems = []string{detail}
		}
		progress(excel.AnalysisProgressEvent{
			Label:       "excel." + label,
			Detail:      detail,
			Current:     current,
			Total:       total,
			ActiveItems: activeItems,
		})
	}
}

func (s *Server) nextProgressTaskID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progressTaskSequence++
	return fmt.Sprintf("%s-%d", prefix, s.progressTaskSequence)
}

func (s *Server) publishProgressTasks(reason string) {
	s.scheduleProgressTasksPublish(reason, false)
}

func (s *Server) publishProgressTasksImmediate(reason string) <-chan struct{} {
	return s.scheduleProgressTasksPublish(reason, true)
}

func (s *Server) scheduleProgressTasksPublish(reason string, immediate bool) <-chan struct{} {
	const interval = 100 * time.Millisecond
	s.mu.Lock()
	if s.progressPublisherClosed {
		s.mu.Unlock()
		return nil
	}
	now := time.Now()
	if !immediate && !s.progressLastPublished.IsZero() {
		remaining := interval - now.Sub(s.progressLastPublished)
		if remaining > 0 {
			s.progressPendingReason = reason
			if s.progressPublishTimer == nil {
				s.progressPublishTimer = time.AfterFunc(remaining, func() {
					s.flushScheduledProgressTasks()
				})
			}
			s.mu.Unlock()
			return nil
		}
	}
	if s.progressPublishTimer != nil {
		s.progressPublishTimer.Stop()
		s.progressPublishTimer = nil
	}
	s.progressPendingReason = ""
	s.progressLastPublished = now
	status := s.progressTasksStatusLocked(reason)
	done := s.enqueueProgressStatusLocked(status)
	s.mu.Unlock()
	return done
}

func (s *Server) flushScheduledProgressTasks() {
	s.mu.Lock()
	if s.progressPublisherClosed {
		s.progressPublishTimer = nil
		s.progressPendingReason = ""
		s.mu.Unlock()
		return
	}
	reason := s.progressPendingReason
	s.progressPendingReason = ""
	s.progressPublishTimer = nil
	s.progressLastPublished = time.Now()
	status := s.progressTasksStatusLocked(reason)
	s.enqueueProgressStatusLocked(status)
	s.mu.Unlock()
}

func (s *Server) stopProgressPublisher() {
	s.mu.Lock()
	s.progressPublisherClosed = true
	if s.progressPublishTimer != nil {
		s.progressPublishTimer.Stop()
		s.progressPublishTimer = nil
	}
	s.progressPendingReason = ""
	if s.progressPendingStatus != nil {
		s.progressPendingStatus.complete()
		s.progressPendingStatus = nil
	}
	s.mu.Unlock()
	s.progressPublishWG.Wait()
}

func (s *Server) enqueueProgressStatusLocked(status map[string]any) <-chan struct{} {
	publication := &progressStatusPublication{status: status, done: make(chan struct{})}
	if s.progressPublishInFlight {
		if pending := s.progressPendingStatus; pending != nil {
			publication.superseded = append(pending.superseded, pending.done)
		}
		s.progressPendingStatus = publication
		return publication.done
	}
	s.progressPublishInFlight = true
	s.progressPublishWG.Add(1)
	go s.runProgressPublisher(publication)
	return publication.done
}

func (s *Server) runProgressPublisher(publication *progressStatusPublication) {
	defer s.progressPublishWG.Done()
	for publication != nil {
		s.reportAsyncRPCWriteError(s.publishStatus(publication.status))
		publication.complete()
		s.mu.Lock()
		if s.progressPublisherClosed {
			if s.progressPendingStatus != nil {
				s.progressPendingStatus.complete()
				s.progressPendingStatus = nil
			}
			s.progressPublishInFlight = false
			s.mu.Unlock()
			return
		}
		publication = s.progressPendingStatus
		s.progressPendingStatus = nil
		if publication == nil {
			s.progressPublishInFlight = false
		}
		s.mu.Unlock()
	}
}

func (s *Server) progressTasksStatusLocked(reason string) map[string]any {
	ids := make([]string, 0, len(s.progressTasks))
	for id := range s.progressTasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tasks := make([]map[string]any, 0, len(ids))
	status := "idle"
	current := 0
	total := 0
	unknownCurrent := 0
	hasUnknownTotal := false
	for _, id := range ids {
		task := s.progressTasks[id]
		if task == nil {
			continue
		}
		if task.Kind == "analyzing" {
			status = "analyzing"
		} else if status == "idle" && task.Kind == "loading" {
			status = "loading"
		}
		if task.Total > 0 {
			current += task.Current
			total += task.Total
		} else {
			hasUnknownTotal = true
			unknownCurrent += task.Current
		}
		published := map[string]any{
			"id": task.ID, "kind": task.Kind, "label": task.Label, "detail": task.Detail,
			"current": task.Current, "total": task.Total, "activeItems": append([]string(nil), task.ActiveItems...),
			"cancellable": task.Cancellable, "state": task.State,
			"startedAt": task.StartedAt, "updatedAt": task.UpdatedAt,
		}
		if task.DocumentURI != "" {
			published["documentUri"] = task.DocumentURI
		}
		if task.HasDocumentVersion {
			published["documentVersion"] = task.DocumentVersion
		}
		tasks = append(tasks, published)
	}
	if hasUnknownTotal {
		current = unknownCurrent
		total = 0
	}
	return map[string]any{
		"status": status, "reason": reason,
		"progress": map[string]int{"current": current, "total": total},
		"tasks":    tasks,
	}
}
