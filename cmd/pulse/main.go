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

	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart/streamlinechart"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// monitor provides a small time that show requests per second and average
// response times, it exposes a small HTTP API to receive metrics.

const (
	ListenHost     string = "127.0.0.1"
	ListenPort     int    = 8090
	HistorySeconds int    = 30
)

// A command that waits for the activity on a channel.
func waitForActivity(sub chan int) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

// Define styles for the chart
var (
	defaultStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("63")) // purple

	graphLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("4")) // blue

	axisStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")) // yellow

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("6")) // cyan
)

type model struct {
	sub      chan int // where we'll receive activity notifications
	rps      int
	spinner  spinner.Model
	quitting bool
	chart    streamlinechart.Model
	zM       *zone.Manager
}

func (m model) Init() tea.Cmd {
	m.chart.DrawXYAxisAndLabel()
	return tea.Batch(
		m.spinner.Tick,
		waitForActivity(m.sub), // wait for activity
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			// Reset the chart
			m.chart.ClearAllData()
			m.chart.Clear()
			m.chart.DrawXYAxisAndLabel()
			return m, nil
		default:
			// Forward other key events to the chart
			var cmd tea.Cmd
			m.chart, cmd = m.chart.Update(msg)
			m.chart.DrawAll()
			return m, cmd
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress {
			if m.zM.Get(m.chart.ZoneID()).InBounds(msg) {
				m.chart.Focus()
			} else {
				m.chart.Blur()
			}
		}
		var cmd tea.Cmd
		m.chart, cmd = m.chart.Update(msg)
		m.chart.DrawAll()
		return m, cmd
	case int:
		m.rps = msg
		// Push the new value to the chart
		m.chart.Push(float64(msg))
		m.chart.Draw()
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
	s := fmt.Sprintf("\n %s Current Visit RPS: %d\n\n", m.spinner.View(), m.rps)

	// Add the chart view with styling
	s += defaultStyle.Render(m.chart.View())

	s += "\n Press 'r' to reset, 'q' or 'ctrl+c' to exit\n"
	s += " Use mouse wheel or pgup/pgdown to zoom, arrow keys to pan\n"

	if m.quitting {
		s += "\n"
	}

	return m.zM.Scan(s) // call zone Manager.Scan() at root model
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

	// Create a new bubblezone Manager to enable mouse support
	zoneManager := zone.New()

	// Create a new streamline chart with appropriate dimensions
	width := 60
	height := 15
	minYValue := 0.0
	maxYValue := 100.0

	// Create the chart with options
	chart := streamlinechart.New(width, height,
		streamlinechart.WithYRange(minYValue, maxYValue),
		streamlinechart.WithAxesStyles(axisStyle, labelStyle),
		streamlinechart.WithStyles(runes.ThinLineStyle, graphLineStyle),
		streamlinechart.WithZoneManager(zoneManager),
	)

	p := tea.NewProgram(model{
		sub:     ch,
		spinner: spinner.New(),
		chart:   chart,
		zM:      zoneManager,
	}, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Println("could not start program:", err)
		os.Exit(1)
	}
	<-ctx.Done()
	stop()
}
