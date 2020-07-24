package tunnel

import (
	"context"
	"fmt"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

type uiModel struct {
	version    string
	hostname   string
	metricsURL string
	proxyURL   string
}

func newUIModel(version, hostname, metricsURL, proxyURL string) *uiModel {
	return &uiModel{
		version:    version,
		hostname:   hostname,
		metricsURL: metricsURL,
		proxyURL:   proxyURL,
	}
}

func (data *uiModel) launchUI(ctx context.Context, logger logger.Service) {
	const steelBlue = "#4682B4"
	const limeGreen = "#00FF00"

	app := tview.NewApplication()

	grid := tview.NewGrid()
	frame := tview.NewFrame(grid)
	header := fmt.Sprintf("cloudflared [::b]%s", data.version)

	frame.AddText(header, true, tview.AlignLeft, tcell.ColorWhite)

	// SetColumns takes a value for each column, representing the size of the column
	// Numbers <= 0 represent proportional widths and positive numbers represent absolute widths
	grid.SetColumns(20, 0)

	// SetRows takes a value for each row, representing the size of the row
	grid.SetRows(2, 2, 1, 1, 1, 2, 1, 0)

	// AddItem takes a primitive tview type, row, column, rowSpan, columnSpan, minGridHeight, minGridWidth, and focus
	grid.AddItem(tview.NewTextView().SetText("Tunnel:"), 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Status:"), 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Connections:"), 2, 0, 4, 1, 0, 0, false)
	grid.AddItem(tview.NewTextView().SetText("Metrics:"), 6, 0, 1, 1, 0, 0, false)

	grid.AddItem(tview.NewTextView().SetText(data.hostname), 0, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText(fmt.Sprintf("[%s]\u2022[white] Proxying to [%s::b]%s", limeGreen, steelBlue, data.proxyURL)), 1, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText("\u2022 #1 "), 2, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText("\u2022 #2 "), 3, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText("\u2022 #3 "), 4, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText("\u2022 #4 "), 5, 1, 1, 1, 0, 0, false)
	grid.AddItem(newDynamicColorTextView().SetText(fmt.Sprintf("Metrics at [%s::b]%s/metrics", steelBlue, data.metricsURL)), 6, 1, 1, 1, 0, 0, false)
	grid.AddItem(tview.NewBox(), 7, 0, 1, 2, 0, 0, false)

	go func() {
		<-ctx.Done()
		app.Stop()
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
