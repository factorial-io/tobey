package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// monitor provides a small time that show requests per second and average
// response times, it exposes a small HTTP API to receive metrics.

const (
	ListenHost string = "127.0.0.1"
	ListenPort int    = 8090
)

// A command that waits for the activity on a channel.
func waitForActivity(sub chan int) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

type model struct {
	sub      chan int // where we'll receive activity notifications
	rps      int
	spinner  spinner.Model
	quitting bool
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		waitForActivity(m.sub), // wait for activity
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		m.quitting = true
		return m, tea.Quit
	case int:
		m.rps = msg.(int)
		return m, waitForActivity(m.sub) // wait for next event
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

func (m model) View() string {
	s := fmt.Sprintf("\n %s Current Visit RPS: %d\n\n Press any key to exit\n", m.spinner.View(), m.rps)
	if m.quitting {
		s += "\n"
	}
	return s
}

func main() {
	slog.Info("Tobey Pulse is starting...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	apirouter := http.NewServeMux()
	ch := make(chan int)

	apirouter.HandleFunc("POST /rps", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// This receives just an integer as plain text. Parse it.

		body, _ := io.ReadAll(r.Body)
		v, _ := strconv.Atoi(string(body))

		ch <- v

		r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})

	apiserver := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", ListenHost, ListenPort),
		Handler: apirouter,
	}
	go func() {
		if err := apiserver.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error.", "error", err)
		}
		slog.Info("Stopped serving new API HTTP connections.")
	}()

	p := tea.NewProgram(model{
		sub:     ch,
		spinner: spinner.New(),
	})

	if _, err := p.Run(); err != nil {
		fmt.Println("could not start program:", err)
		os.Exit(1)
	}
	<-ctx.Done()
	stop()
}
