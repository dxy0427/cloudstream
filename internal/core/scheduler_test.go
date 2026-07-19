package core

import (
	"cloudstream/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func TestBlockTasksIfIdleIsAtomic(t *testing.T) {
	taskMutex.Lock()
	runningTasks[2] = func() {}
	runningTaskDone[2] = make(chan struct{})
	runningTaskPaths[2] = "/tmp/task-2"
	taskMutex.Unlock()
	t.Cleanup(func() {
		taskMutex.Lock()
		delete(runningTasks, 2)
		delete(runningTaskDone, 2)
		delete(runningTaskPaths, 2)
		delete(blockedTasks, 1)
		delete(blockedTasks, 2)
		taskMutex.Unlock()
	})

	if BlockTasksIfIdle([]uint{1, 2}) {
		t.Fatal("tasks were blocked while one task was running")
	}
	taskMutex.Lock()
	_, firstBlocked := blockedTasks[1]
	taskMutex.Unlock()
	if firstBlocked {
		t.Fatal("failed atomic block left an earlier task blocked")
	}

	taskMutex.Lock()
	delete(runningTasks, 2)
	delete(runningTaskDone, 2)
	delete(runningTaskPaths, 2)
	taskMutex.Unlock()
	if !BlockTasksIfIdle([]uint{1, 2}) {
		t.Fatal("idle tasks could not be blocked")
	}
	if ctx, ok := reserveTaskRun(1, "/tmp/task-1"); ok || ctx != nil {
		t.Fatal("blocked task was allowed to start")
	}
}

func TestOverlappingTaskIDResolvesMissingSymlinkDescendants(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real")
	if err := os.Mkdir(realPath, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(root, "alias")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	scheduledTask := models.Task{LocalPath: filepath.Join(realPath, "future")}
	scheduledTask.ID = 3
	scheduled := []models.Task{scheduledTask}
	task := models.Task{LocalPath: filepath.Join(aliasPath, "future", "child")}
	task.ID = 4
	if got := overlappingTaskID(task, scheduled); got != 3 {
		t.Fatalf("conflicting task ID=%d", got)
	}
}

func TestReserveTaskRunRejectsOverlappingRunningPath(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "media")
	if ctx, ok := reserveTaskRun(101, firstPath); !ok || ctx == nil {
		t.Fatal("first task could not reserve its output path")
	}
	t.Cleanup(func() { finishTaskRun(101) })
	if ctx, ok := reserveTaskRun(102, filepath.Join(firstPath, "child")); ok || ctx != nil {
		t.Fatal("overlapping task reserved a running output path")
	}
}
