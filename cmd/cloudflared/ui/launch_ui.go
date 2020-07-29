package ui

import (
	"context"
	"fmt"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

type connState struct {
	location string
	state    status
}

type status int

const (
	Disconnected status = iota
	Connected
)

type ConnEvent struct {
	Index     uint8
	EventType status
	Location  string
}

type uiModel struct {
	version     string
	hostname    string
	metricsURL  string
	proxyURL    string
	connections []connState
}

type palette struct {
	url          string
	connected    string
	defaultText  string
	disconnected string
}

func NewUIModel(version, hostname, metricsURL, proxyURL string, haConnections int) *uiModel {
	return &uiModel{
		version:     version,
		hostname:    hostname,
		metricsURL:  metricsURL,
		proxyURL:    proxyURL,
		connections: make([]connState, haConnections),
	}
}

func (data *uiModel) LaunchUI(ctx context.Context, logger logger.Service, connEventChan <-chan ConnEvent) {
	palette := palette{url: "#4682B4", connected: "#00FF00", defaultText: "white", disconnected: "red"}

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
	grid.SetRows(1, 1, 11, 1, 0)

	// AddItem takes a primitive tview type, row, column, rowSpan, columnSpan, minGridHeight, minGridWidth, and focus
	grid.AddItem(tview.NewTextView().SetText("Tunnel:"), 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Status:"), 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Connections:"), 2, 0, 1, 1, 0, 0, false)

	grid.AddItem(tview.NewTextView().SetText("Metrics:"), 3, 0, 1, 1, 0, 0, false)

	grid.AddItem(tview.NewTextView().SetText(data.hostname), 0, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText(fmt.Sprintf("[%s]\u2022[%s] Proxying to [%s::b]%s", palette.connected, palette.defaultText, palette.url, data.proxyURL)), 1, 1, 1, 1, 0, 0, false)

	grid.AddItem(connTable, 2, 1, 1, 1, 0, 0, false)

	grid.AddItem(newDynamicColorTextView().SetText(fmt.Sprintf("Metrics at [%s::b]%s/metrics", palette.url, data.metricsURL)), 3, 1, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewBox(), 4, 0, 1, 2, 0, 0, false)

	go func() {
		for {
			select {
			case <-ctx.Done():
				app.Stop()
				return
			case conn := <-connEventChan:
				switch conn.EventType {
				case Connected:
					data.setConnTableCell(conn, connTable, palette)
				case Disconnected:
					data.changeConnStatus(conn, connTable, logger, palette)
				}
			}
			app.Draw()
		}
	}()

	go func() {
		if err := app.SetRoot(frame, true).Run(); err != nil {
			logger.Errorf("Error launching UI: %s", err)
		}
	}()
}

func newDynamicColorTextView() *tview.TextView {
	return tview.NewTextView().SetDynamicColors(true)
}

func (data *uiModel) changeConnStatus(conn ConnEvent, table *tview.Table, logger logger.Service, palette palette) {
	index := int(conn.Index)
	// Get table row and connection location
	connState := data.getConnState(index)
	// Check if connection is already displayed in UI
	if connState == nil {
		logger.Info("Connection is not in the UI table")
		return
	}

	connState.state = Disconnected
	connectionNum := index + 1
	// Get table cell
	cell := table.GetCell(index, 0)
	// Change text in cell to have red dot in front
	text := newCellText(palette, connectionNum, connState.location, false)
	cell.SetText(text)
}

// Return connection location and row in UI table
func (data *uiModel) getConnState(connID int) *connState {
	if connID < len(data.connections) {
		return &data.connections[connID]
	}

	return nil
}

func (data *uiModel) setConnTableCell(conn ConnEvent, table *tview.Table, palette palette) {
	index := int(conn.Index)
	connectionNum := index + 1

	// Update slice to keep track of connection location and state in UI table
	data.connections[index].state = Connected
	data.connections[index].location = conn.Location

	// Update text in table cell to show disconnected state
	text := newCellText(palette, connectionNum, conn.Location, true)
	cell := tview.NewTableCell(text)
	table.SetCell(index, 0, cell)
}

func newCellText(palette palette, connectionNum int, location string, connected bool) string {
	const connFmtString = "[%s]\u2022[%s] #%d: %s"

	dotColor := palette.disconnected
	if connected {
		dotColor = palette.connected
	}

	return fmt.Sprintf(connFmtString, dotColor, palette.defaultText, connectionNum, location)
}
