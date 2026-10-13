package council

import (
	"fmt"
	"path/filepath"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ui"
)

// todoFor links a Task Council to its Task's todo, or creates one for a
// standalone Council. Run Councils keep their existing Run tracking.
func todoFor(env *dispatch.Env, c *Council) {
	if env.Solo == nil || c.Todo != 0 {
		return
	}
	if c.Task != "" {
		id := c.Task
		if c.Run != "" {
			id = filepath.Join(c.Run, c.Task)
		}
		if folder, ok := dispatch.FindTask(env.Root, id); ok {
			if task, err := dispatch.LoadTask(folder); err == nil {
				c.Todo = task.Todo
			}
		}
	} else if c.Run == "" {
		body := fmt.Sprintf("Maestro council `%s`. Question: `%s`.\n\n%s\n", c.Slug, relOrAbs(c.Project, filepath.Join(c.Path, "question.md")), c.Question)
		id, err := env.Solo.Client.CreateTodo(env.Solo.ProjectID, c.Question, body, []string{"maestro"})
		if err != nil {
			ui.Warnf("solo council todo: %v", err)
			return
		}
		c.Todo = id
	}
	if err := c.Save(); err != nil {
		ui.Warnf("solo council todo: %v", err)
	}
}

// mergeComment posts one Merge and completes a standalone Council's todo.
func mergeComment(env *dispatch.Env, c *Council) {
	if env.Solo == nil || c.Todo == 0 {
		return
	}
	if err := env.Solo.Client.Comment(env.Solo.ProjectID, c.Todo, Merge(c)); err != nil {
		ui.Warnf("solo council comment: %v", err)
		return
	}
	if c.Task == "" && c.Run == "" {
		if err := env.Solo.Client.CompleteTodo(env.Solo.ProjectID, c.Todo); err != nil {
			ui.Warnf("solo council complete: %v", err)
		}
	}
}
