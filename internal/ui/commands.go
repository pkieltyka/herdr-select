package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/pkieltyka/herdr-select/internal/herdr"
)

type action int

const (
	refresh action = iota
	create
	open
	prepareStop
	prepareDelete
	mutate
)

type target struct {
	Name            string
	Delete, Running bool
}
type result struct {
	id                   uint64
	kind                 action
	sessions             []herdr.Session
	listed               bool
	listedAt             time.Time
	err                  error
	refreshErr           error
	confirmation         *target
	launch               *LaunchRequest
	notice               string
	stoppedButNotDeleted bool
}

func find(sessions []herdr.Session, name string) (herdr.Session, bool) {
	for _, s := range sessions {
		if s.Name == name {
			return s, true
		}
	}
	return herdr.Session{}, false
}

func operation(backend herdr.Backend, id uint64, kind action, name string, confirmed target) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		r := result{id: id, kind: kind}
		sessions, err := backend.List(ctx)
		if err != nil {
			r.err = err
			if kind == mutate {
				r.refreshErr = err
			}
			return r
		}
		r.sessions, r.listed = sessions, true
		r.listedAt = time.Now()
		if kind == refresh {
			return r
		}
		if kind == create {
			if r.err = herdr.ValidateNewName(name, sessions); r.err == nil {
				r.launch = &LaunchRequest{Name: name, Create: true}
			}
			return r
		}
		s, exists := find(sessions, name)
		if !exists {
			r.notice = fmt.Sprintf("Session ‘%s’ no longer exists.", name)
			return r
		}
		if kind == open {
			r.launch = &LaunchRequest{Name: name}
			return r
		}
		deleting := kind == prepareDelete || kind == mutate && confirmed.Delete
		if deleting && (s.Default || name == "default") {
			r.err = fmt.Errorf("The default session cannot be deleted.")
			return r
		}
		if s.ConnectionError != "" {
			r.err = fmt.Errorf("Session ‘%s’ is unavailable: %s", name, s.ConnectionError)
			return r
		}
		if kind == prepareStop || kind == prepareDelete {
			if !deleting && !s.Running {
				r.notice = fmt.Sprintf("Session ‘%s’ is already stopped.", name)
				return r
			}
			r.confirmation = &target{Name: name, Delete: deleting, Running: s.Running}
			return r
		}
		if deleting && s.Running && !confirmed.Running {
			r.confirmation = &target{Name: name, Delete: true, Running: true}
			r.notice = "This session is now running. Confirm stopping its work before deletion."
			return r
		}
		stopped := false
		if s.Running {
			r.err = backend.Stop(ctx, name)
			stopped = r.err == nil
		}
		if deleting && r.err == nil {
			if stopped {
				var after []herdr.Session
				after, r.err = backend.List(ctx)
				if r.err == nil {
					current, ok := find(after, name)
					switch {
					case !ok:
						r.notice = fmt.Sprintf("Session ‘%s’ no longer exists.", name)
					case current.Running || current.ConnectionError != "":
						r.err = fmt.Errorf("The session restarted or is unavailable; deletion was canceled.")
					default:
						r.err = backend.Delete(ctx, name)
					}
				}
			} else {
				r.err = backend.Delete(ctx, name)
			}
			r.stoppedButNotDeleted = stopped && r.err != nil
		}
		if r.err == nil && r.notice == "" {
			switch {
			case deleting:
				r.notice = fmt.Sprintf("Deleted session ‘%s’.", name)
			case stopped:
				r.notice = fmt.Sprintf("Stopped session ‘%s’.", name)
			default:
				r.notice = fmt.Sprintf("Session ‘%s’ is already stopped.", name)
			}
		}
		// Every attempted mutation gets a fresh authoritative list, including uncertain failures.
		r.sessions, r.refreshErr = backend.List(ctx)
		r.listed = r.refreshErr == nil
		if r.listed {
			r.listedAt = time.Now()
		}
		return r
	}
}
