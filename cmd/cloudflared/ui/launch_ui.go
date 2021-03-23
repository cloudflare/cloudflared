package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
)

type connState struct {
	location string
}

type uiModel struct {
	version       string
	edgeURL       string
	metricsURL    string
	localServices []string
	connections   []connState
}

type palette struct {
	url          string
	connected    string
	defaultText  string
	disconnected string
	reconnecting string
	unregistered string
}

func NewUIModel(version, hostname, metricsURL string, ing *ingress.Ingress, haConnections int) *uiModel {
	localServices := make([]string, len(ing.Rules))
	for i, rule := range ing.Rules {
		localServices[i] = rule.Service.String()
	}
	return &uiModel{
		version:       version,
		edgeURL:       hostname,
		metricsURL:    metricsURL,
		localServices: localServices,
		connections:   make([]connState, haConnections),
	}
}

func (data *uiModel) Launch(
	ctx context.Context,
	log, transportLog *zerolog.Logger,
) connection.EventSink {
	// Configure the logger to stream logs into the textview

	// Add TextView as a group to write output to
	logTextView := NewDynamicColorTextView()
	// TODO: Format log for UI
	//log.Add(logTextView, logger.NewUIFormatter(time.RFC3339), logLevels...)
	//transportLog.Add(logTextView, logger.NewUIFormatter(time.RFC3339), logLevels...)

	// Construct the UI
	palette := palette{
		url:          "lightblue",
		connected:    "lime",
		defaultText:  "white",
		disconnected: "red",
		reconnecting: "orange",
		unregistered: "orange",
	}

	app := tview.NewApplication()

	grid := tview.NewGrid().SetGap(1, 0)
	frame := tview.NewFrame(grid)
	header := fmt.Sprintf("cloudflared [::b]%s", data.version)

	frame.AddText(header, true, tview.AlignLeft, tcell.ColorWhite)

	// Create table to store connection info and status
	connTable := tview.NewTable()
	// SetColumns takes a value for each column, representing the size of the column
	// Numbers <= 0 represent proportional widths and positive numbers represent absolute widths
	grid.SetColumns(20, 0)

	// SetRows takes a value for each row, representing the size of the row
	grid.SetRows(1, 1, len(data.connections), 1, 0)

	// AddItem takes a primitive tview type, row, column, rowSpan, columnSpan, minGridHeight, minGridWidth, and focus
	grid.AddItem(tview.NewTextView().SetText("Tunnel:"), 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Status:"), 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Connections:"), 2, 0, 1, 1, 0, 0, false)

	grid.AddItem(tview.NewTextView().SetText("Metrics:"), 3, 0, 1, 1, 0, 0, false)

	tunnelHostText := tview.NewTextView().SetText(data.edgeURL)

	grid.AddItem(tunnelHostText, 0, 1, 1, 1, 0, 0, false)
	status := fmt.Sprintf("[%s]\u2022[%s] Proxying to [%s::b]%s", palette.connected, palette.defaultText, palette.url, strings.Join(data.localServices, ", "))
	grid.AddItem(NewDynamicColorTextView().SetText(status), 1, 1, 1, 1, 0, 0, false)

	grid.AddItem(connTable, 2, 1, 1, 1, 0, 0, false)

	grid.AddItem(NewDynamicColorTextView().SetText(fmt.Sprintf("Metrics at [%s::b]http://%s/metrics", palette.url, data.metricsURL)), 3, 1, 1, 1, 0, 0, false)

	// Add TextView to stream logs
	// Logs are displayed in a new grid so a border can be set around them
	logGrid := tview.NewGrid().SetBorders(true).AddItem(logTextView.SetChangedFunc(handleNewText(app, logTextView)), 0, 0, 5, 2, 0, 0, false)
	// LogFrame holds the Logs header as well as the grid with the textView for streamed logs
	logFrame := tview.NewFrame(logGrid).AddText("[::b]Logs:[::-]", true, tview.AlignLeft, tcell.ColorWhite).SetBorders(0, 0, 0, 0, 0, 0)
	// Footer for log frame
	logFrame.AddText("[::d]Use Ctrl+C to exit[::-]", false, tview.AlignRight, tcell.ColorWhite)
	grid.AddItem(logFrame, 4, 0, 5, 2, 0, 0, false)

	go func() {
		<-ctx.Done()
		app.Stop()
		return
	}()

	go func() {
		if err := app.SetRoot(frame, true).Run(); err != nil {
			log.Error().Msgf("Error launching UI: %s", err)
		}
	}()

	return connection.EventSinkFunc(func(event connection.Event) {
		switch event.EventType {
		case connection.Connected:
			data.setConnTableCell(event, connTable, palette)
		case connection.Disconnected, connection.Reconnecting, connection.Unregistering:
			data.changeConnStatus(event, connTable, log, palette)
		case connection.SetURL:
			tunnelHostText.SetText(event.URL)
			data.edgeURL = event.URL
		case connection.RegisteringTunnel:
			if data.edgeURL == "" {
				tunnelHostText.SetText(fmt.Sprintf("Registering tunnel connection %d...", event.Index))
			}
		}
		app.Draw()
	})
}

func NewDynamicColorTextView() *tview.TextView {
	return tview.NewTextView().SetDynamicColors(true)
}

// Re-draws application when new logs are streamed to UI
func handleNewText(app *tview.Application, logTextView *tview.TextView) func() {
	return func() {
		app.Draw()
		// SetFocus to enable scrolling in textview
		app.SetFocus(logTextView)
	}
}

func (data *uiModel) changeConnStatus(event connection.Event, table *tview.Table, log *zerolog.Logger, palette palette) {
	index := int(event.Index)
	// Get connection location and state
	connState := data.getConnState(index)
	// Check if connection is already displayed in UI
	if connState == nil {
		log.Info().Msg("Connection is not in the UI table")
		return
	}

	locationState := event.Location

	if event.EventType == connection.Reconnecting {
		locationState = "Reconnecting..."
	}

	connectionNum := index + 1
	// Get table cell
	cell := table.GetCell(index, 0)
	// Change dot color in front of text as well as location state
	text := newCellText(palette, connectionNum, locationState, event.EventType)
	cell.SetText(text)
}

// Return connection location and row in UI table
func (data *uiModel) getConnState(connID int) *connState {
	if connID < len(data.connections) {
		return &data.connections[connID]
	}

	return nil
}

func (data *uiModel) setConnTableCell(event connection.Event, table *tview.Table, palette palette) {
	index := int(event.Index)
	connectionNum := index + 1

	// Update slice to keep track of connection location and state in UI table
	data.connections[index].location = event.Location

	// Update text in table cell to show disconnected state
	text := newCellText(palette, connectionNum, event.Location, event.EventType)
	cell := tview.NewTableCell(text)
	table.SetCell(index, 0, cell)
}

func newCellText(palette palette, connectionNum int, location string, connectedStatus connection.Status) string {
	// HA connection indicator formatted as: "• #<CONNECTION_INDEX>: <COLO>",
	//  where the left middle dot's color depends on the status of the connection
	const connFmtString = "[%s]\u2022[%s] #%d: %s"

	var dotColor string
	switch connectedStatus {
	case connection.Connected:
		dotColor = palette.connected
	case connection.Disconnected:
		dotColor = palette.disconnected
	case connection.Reconnecting:
		dotColor = palette.reconnecting
	case connection.Unregistering:
		dotColor = palette.unregistered
	}

	return fmt.Sprintf(connFmtString, dotColor, palette.defaultText, connectionNum, location)
}
